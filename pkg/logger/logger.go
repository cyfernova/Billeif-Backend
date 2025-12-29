package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger struct {
	*zap.SugaredLogger
}

var globalLogger *Logger

func New() *Logger {
	return NewWithEnv(os.Getenv("ENVIRONMENT"))
}

func NewWithEnv(env string) *Logger {
	var zapLogger *zap.Logger
	var err error

	if env == "prod" {
		zapLogger, err = zap.NewProduction()
	} else {
		config := zap.NewDevelopmentConfig()
		config.EncoderConfig.TimeKey = "timestamp"
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		zapLogger, err = config.Build()
	}

	if err != nil {
		zapLogger = zap.NewNop()
	}

	return &Logger{SugaredLogger: zapLogger.Sugar()}
}

func Init(env string) error {
	globalLogger = NewWithEnv(env)
	return nil
}

func Global() *Logger {
	if globalLogger == nil {
		globalLogger = New()
	}
	return globalLogger
}

func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Infow(msg, keysAndValues...)
}

func (l *Logger) Debug(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Debugw(msg, keysAndValues...)
}

func (l *Logger) Warn(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Warnw(msg, keysAndValues...)
}

func (l *Logger) Error(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Errorw(msg, keysAndValues...)
}

func (l *Logger) Fatal(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Fatalw(msg, keysAndValues...)
}

func (l *Logger) With(keysAndValues ...interface{}) *Logger {
	return &Logger{SugaredLogger: l.SugaredLogger.With(keysAndValues...)}
}

func (l *Logger) Sync() {
	_ = l.SugaredLogger.Sync()
}
