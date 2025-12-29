package logger

import (
	"context"
)

type contextKey string

const loggerKey contextKey = "logger"

func FromContext(ctx context.Context) *Logger {
	if logger, ok := ctx.Value(loggerKey).(*Logger); ok {
		return logger
	}
	return Global()
}

func ToContext(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

func WithFieldsFromContext(ctx context.Context, keysAndValues ...interface{}) *Logger {
	logger := FromContext(ctx)
	return logger.With(keysAndValues...)
}
