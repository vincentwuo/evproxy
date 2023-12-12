package util

import (
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var once sync.Once

type LogLevel int

const (
	_ LogLevel = iota
	LOG_DEBUG_LEVEL
	LOG_INFO_LEVEL
)

var logger *zap.Logger
var loggerConfig *zap.Config

func initLogger() {
	once.Do(func() {
		loggerConfig = &zap.Config{Encoding: "console",
			Level:       zap.NewAtomicLevelAt(zapcore.InfoLevel),
			OutputPaths: []string{"stdout"},
			EncoderConfig: zapcore.EncoderConfig{
				MessageKey: "msg",

				LevelKey:    "level",
				EncodeLevel: zapcore.CapitalLevelEncoder,

				TimeKey:    "time",
				EncodeTime: zapcore.ISO8601TimeEncoder,
			}}
		loggerConfig.EncoderConfig.EncodeTime = zapcore.RFC3339TimeEncoder
		var err error
		logger, err = loggerConfig.Build()
		if err != nil {
			panic(err)
		}
	})

}

func Logger() *zap.Logger {
	if loggerConfig == nil {
		initLogger()
	}
	return logger
}

func LoggerLevel(lv LogLevel) {
	if loggerConfig == nil {
		initLogger()
	}
	switch lv {
	case LOG_DEBUG_LEVEL:
		loggerConfig.Level.SetLevel(zapcore.DebugLevel)
	case LOG_INFO_LEVEL:
		loggerConfig.Level.SetLevel(zapcore.InfoLevel)
	}
}

// LoggerOutputPaths set where the logs are written to.
// Paths receive values like "stdout" ,"stderr" or "path/to/file"
func LoggerOutputPaths(paths []string) {
	if loggerConfig == nil {
		initLogger()
	}
	loggerConfig.OutputPaths = paths
	logger, _ = loggerConfig.Build()
}

func DebugSugarLog(args ...interface{}) {
	if loggerConfig == nil {
		initLogger()
	}
	logger.Sugar().Debug(args...)
}
