package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	promver "github.com/prometheus/common/version"
	"github.com/spotahome/kooper/log"
	"github.com/spotahome/kooper/monitoring/metrics"
	"github.com/spotahome/kooper/operator/controller"
	"github.com/spotahome/kooper/operator/handler"
	"github.com/spotahome/kooper/operator/retrieve"
	"gopkg.in/alecthomas/kingpin.v2"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

var (
	outdir      = kingpin.Flag("outdir", "Directory to output dashboards into").Default("/tmp/dashboards").ExistingDir()
	namespace   = kingpin.Flag("namespace", "Namespace to watch").Default(corev1.NamespaceAll).Short('n').String()
	selector    = kingpin.Flag("selector", "Label selector for config maps containing dashboards").Default("grafana-dashboard=true").String()
	metricsAddr = kingpin.Flag("metrics-addr", "Address to bind metrics server to").Default(":8080").String()
	logLevel    = kingpin.Flag("log-level", "Minimum logging level to output").Envar("LOG_LEVEL").Default("info").Enum("info", "warn", "error")
	logFormat   = kingpin.Flag("log-format", "Format of log output").Default("logfmt").Enum("logfmt", "json")
	version     = kingpin.Flag("version", "Print version and exit").Bool()
)

const metricsNamespace = "dashboard_loader"

var (
	createErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "create_errors_total",
		Help:      "Number of errors while creating dashboard files",
	})
	deleteErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Name:      "delete_errors_total",
		Help:      "Number of errors while deleting dashboard files",
	})
)

func init() {
	prometheus.MustRegister(createErrors)
	prometheus.MustRegister(deleteErrors)
	prometheus.MustRegister(promver.NewCollector("grafana_loader"))
}

func main() {
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	if *version {
		fmt.Println(promver.Print("grafana_loader"))
		os.Exit(0)
	}

	logger := newLogger(*logLevel, *logFormat)

	config, err := rest.InClusterConfig()
	if err != nil {
		kubehome := filepath.Join(homedir.HomeDir(), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubehome)
		if err != nil {
			logger.Errorf("Error loading Kubernetes configuration: %v", err)
			os.Exit(1)
		}
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		logger.Errorf("Error creating Kubernetes client: %v", err)
		os.Exit(1)
	}

	retr := &retrieve.Resource{
		Object: &corev1.ConfigMap{},
		ListerWatcher: &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = *selector
				return client.CoreV1().ConfigMaps(*namespace).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = *selector
				return client.CoreV1().ConfigMaps(*namespace).Watch(options)
			},
		},
	}

	eh := eventHandler{logger: logger}
	h := &handler.HandlerFunc{
		AddFunc:    eh.addConfigMap,
		DeleteFunc: eh.deleteConfigMap,
	}

	m := metrics.NewPrometheus(metricsNamespace, prometheus.DefaultRegisterer)
	// Create the controller that will refresh every 30 seconds.
	ctrl := controller.NewSequential(30*time.Second, h, retr, m, logger)

	// Start metrics server
	go func() {
		err := http.ListenAndServe(*metricsAddr, promhttp.Handler())
		if err != nil {
			logger.Errorf("Failed to start metrics server: %v", err)
			os.Exit(1)
		}
	}()
	// Start controller
	stopC := make(chan struct{})
	if err := ctrl.Run(stopC); err != nil {
		logger.Errorf("Error running controller: %v", err)
		os.Exit(1)
	}
}

type eventHandler struct {
	logger log.Logger
}

func (eh eventHandler) addConfigMap(obj runtime.Object) error {
	cm := obj.(*corev1.ConfigMap)
	eh.logger.Infof("ConfigMap added: %s/%s", cm.Namespace, cm.Name)

	dir := filepath.Join(*outdir, cm.Namespace, cm.Name)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		eh.logger.Errorf("Cannot create dashboard directory for %s/%s: %v", cm.Namespace, cm.Name, err)
		createErrors.Inc()
		return err
	}

	existingFiles, err := ioutil.ReadDir(dir)
	if err != nil {
		eh.logger.Errorf("Cannot list dashboards for %s/%s: %v", cm.Namespace, cm.Name, err)
		createErrors.Inc()
		return err
	}
	filesToRemove := make(map[string]bool)
	for _, f := range existingFiles {
		filesToRemove[f.Name()] = true
	}

	for name, content := range cm.Data {
		if filepath.Ext(name) != ".json" {
			eh.logger.Infof("Skipping %s in %s/%s, does not have .json extension", name, cm.Namespace, cm.Name)
			continue
		}
		// ConfigMap data items will result in files named like this:
		// default/my-dashboard->dash.json  => {outdir}/default/my-dashboard/dash.json
		err := ioutil.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
		if err != nil {
			eh.logger.Errorf("Failed to write %s/%s: %v", cm.Namespace, cm.Name, err)
			createErrors.Inc()
			continue
		}

		delete(filesToRemove, name)
	}

	for f := range filesToRemove {
		path := filepath.Join(dir, f)
		err := os.Remove(path)
		if err != nil {
			eh.logger.Errorf("Failed to remove outdated file %s: %v", path, err)
			deleteErrors.Inc()
			continue
		}
	}

	return nil
}

func (eh eventHandler) deleteConfigMap(s string) error {
	eh.logger.Infof("ConfigMap deleted: %s", s)
	dir := filepath.Join(*outdir, s)
	err := os.RemoveAll(dir)
	if err != nil {
		eh.logger.Errorf("Failed to remove dashboards from %s: %v", s, err)
		deleteErrors.Inc()
		return err
	}
	return nil
}
