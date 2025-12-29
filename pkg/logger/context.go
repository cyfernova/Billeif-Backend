package logger

import (
	"context"

	"go.uber.org/zap"
)

type contextKey string

const loggerKey contextKey = "logger"

func FromContext(ctx context.Context) *zap.Logger {
	if logger, ok := ctx.Value(loggerKey).(*zap.Logger); ok {
		return logger
	}
	return globalLogger
}

func ToContext(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

func WithFields(ctx context.Context, fields ...zap.Field) *zap.Logger {
	logger := FromContext(ctx)
	if logger == nil {
		return globalLogger
	}
	return logger.With(fields...)
}
