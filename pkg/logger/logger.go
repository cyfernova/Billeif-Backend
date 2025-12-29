package logger

import (
	"os"

	"go.uber.org/zap"
)

var globalLogger *zap.Logger

func Init(env string) error {
	var err error
	var logger *zap.Logger

	if env == "prod" {
		logger, err = zap.NewProduction()
	} else {
		config := zap.NewDevelopmentConfig()
		config.EncoderConfig.TimeKey = "timestamp"
		config.EncoderConfig.EncodeTime = zap.TimeEncoderOfLayout("2006-01-02T15:04:05.000Z")
		logger, err = config.Build()
	}

	if err != nil {
		return err
	}

	globalLogger = logger
	return nil
}

func Sync() {
	if globalLogger != nil {
		_ = globalLogger.Sync()
	}
}

func Info(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Info(msg, fields...)
	}
}

func Debug(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Debug(msg, fields...)
	}
}

func Warn(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Warn(msg, fields...)
	}
}

func Error(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Error(msg, fields...)
	}
}

func Fatal(msg string, fields ...zap.Field) {
	if globalLogger != nil {
		globalLogger.Fatal(msg, fields...)
	}
	os.Exit(1)
}

func WithRequestID(requestID string) zap.Field {
	return zap.String("request_id", requestID)
}

func WithUserID(userID string) zap.Field {
	return zap.String("user_id", userID)
}

func WithError(err error) zap.Field {
	return zap.Error(err)
}
