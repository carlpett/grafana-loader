package main

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

/*
Kooper's logger interface:
type Logger interface {
    Infof(format string, args ...interface{})
    Warningf(format string, args ...interface{})
    Errorf(format string, args ...interface{})
}
*/

type loggerImpl struct {
	z *zap.SugaredLogger
}

func (l *loggerImpl) Infof(format string, args ...interface{}) {
	l.z.Infof(format, args...)
}
func (l *loggerImpl) Infow(msg string, keysAndValues ...interface{}) {
	l.z.Infow(msg, keysAndValues...)
}

func (l *loggerImpl) Warningf(format string, args ...interface{}) {
	l.z.Warnf(format, args...)
}
func (l *loggerImpl) Warningw(msg string, keysAndValues ...interface{}) {
	l.z.Warnw(msg, keysAndValues...)
}

func (l *loggerImpl) Errorf(format string, args ...interface{}) {
	l.z.Errorf(format, args...)
}
func (l *loggerImpl) Errorw(msg string, keysAndValues ...interface{}) {
	l.z.Errorw(msg, keysAndValues...)
}

func newLogger(level, format string) *loggerImpl {
	var zaplevel zapcore.Level
	unrecognizedLevel := false
	switch strings.ToLower(level) {
	case "", "info":
		zaplevel = zapcore.InfoLevel
	case "warn":
		zaplevel = zapcore.WarnLevel
	case "error":
		zaplevel = zapcore.ErrorLevel
	default:
		zaplevel = zapcore.InfoLevel
		unrecognizedLevel = true
	}

	consoleOutput := zapcore.Lock(os.Stdout)
	threshold := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= zaplevel
	})

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var enc zapcore.Encoder
	switch format {
	case "json":
		enc = zapcore.NewJSONEncoder(encoderConfig)
	case "logfmt":
		enc = zapcore.NewConsoleEncoder(encoderConfig)
	}
	core := zapcore.NewCore(enc, consoleOutput, threshold)
	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	defer logger.Sync()

	li := &loggerImpl{
		z: logger.Sugar(),
	}
	if unrecognizedLevel {
		li.Warningw("Unrecognized value of log level, defaulting to info", "level", level)
	}
	return li
}
