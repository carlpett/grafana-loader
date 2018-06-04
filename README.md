# grafana-loader [![CircleCI](https://circleci.com/gh/carlpett/grafana-loader.svg?style=shield)](https://circleci.com/gh/carlpett/grafana-loader) [![DockerHub](https://img.shields.io/docker/build/carlpett/grafana-loader.svg?style=shield)](https://hub.docker.com/r/carlpett/grafana-loader/)

Syncs Grafana v5+ dashboard definitions from Kubernetes ConfigMaps.

*NOTE:* This is alpha version software. It may eat your dashboards. 

## Usage
To deploy, ensure the following file is added to Grafana's [dashboard provisioning directory](http://docs.grafana.org/administration/provisioning/) (`/etc/grafana/provisioning/dashboards` by default):

```yaml
apiVersion: 1

providers:
- name: 'default'
  orgId: 1
  folder: ''
  type: file
  disableDeletion: true
  options:
    path: /tmp/dashboards
```

Then run grafana-loader as a sidecar in the Grafana pod:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  # ...
spec:
  # ...
  template:
    spec:
      containers:
      - name: grafana
      	# ...
        volumeMounts:
        - name: dashboards
          mountPath: /tmp/dashboards
      - name: dashboard-loader
        image: "carlpett/dashboard-loader:v0.0.1"
        imagePullPolicy: IfNotPresent
        ports:
        - name: metrics
          containerPort: 8080
          protocol: TCP
        securityContext:
          runAsUser: 1000
          fsGroup: 1000
        resources:
          requests:
            cpu: 10m
            memory: 10Mi
        volumeMounts:
          - name: dashboards
            mountPath: /tmp/dashboards
      volumes:
      - name: dashboards
        emptyDir: {}
```

This will keep all ConfigMaps labeled `grafana-dashboard=true` synced in the `/tmp/dashboards` directory.

## Advanced usages
Download the [latest release](https://github.com/carlpett/grafana-loader/releases), pull [the Docker image](https://hub.docker.com/r/carlpett/grafana-loader/) or follow the instructions below for building the source.

There is a `--help` flag for listing the available flags.

## Building from source
`go get -u github.com/carlpett/grafana-loader` and then `make build`.
