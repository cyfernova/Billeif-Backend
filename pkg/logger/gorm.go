package logger

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	sqlStringPattern = regexp.MustCompile(`'[^']*'`)
	sqlNumberPattern = regexp.MustCompile(`\b\d+\b`)
)

type GORMOptions struct {
	Environment               string
	SlowThreshold             time.Duration
	Level                     string
	IgnoreRecordNotFoundError bool
	IncludeQuery              bool
}

type GORMLogger struct {
	log                        *Logger
	level                      gormlogger.LogLevel
	slowThreshold              time.Duration
	ignoreRecordNotFoundErrors bool
	includeQuery               bool
}

func NewGORMLogger(log *Logger, opts GORMOptions) gormlogger.Interface {
	if log == nil {
		log = Global()
	}

	slowThreshold := opts.SlowThreshold
	if slowThreshold <= 0 {
		slowThreshold = 200 * time.Millisecond
	}

	return &GORMLogger{
		log:                        log.Named("gorm"),
		level:                      parseGORMLevel(opts.Level),
		slowThreshold:              slowThreshold,
		ignoreRecordNotFoundErrors: opts.IgnoreRecordNotFoundError,
		includeQuery:               opts.IncludeQuery,
	}
}

func (g *GORMLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	copied := *g
	copied.level = level
	return &copied
}

func (g *GORMLogger) Info(ctx context.Context, msg string, args ...interface{}) {
	if g.level < gormlogger.Info {
		return
	}
	g.withContext(ctx).Info("gorm info", "message", msg, "args", sanitizeArgs(args))
}

func (g *GORMLogger) Warn(ctx context.Context, msg string, args ...interface{}) {
	if g.level < gormlogger.Warn {
		return
	}
	g.withContext(ctx).Warn("gorm warning", "message", msg, "args", sanitizeArgs(args))
}

func (g *GORMLogger) Error(ctx context.Context, msg string, args ...interface{}) {
	if g.level < gormlogger.Error {
		return
	}
	g.withContext(ctx).Error("gorm error", "message", msg, "args", sanitizeArgs(args))
}

func (g *GORMLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if g.level == gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()
	fields := []interface{}{
		"elapsed_ms", elapsed.Milliseconds(),
		"rows", rows,
		"sql", g.formatSQL(sql),
	}

	switch {
	case err != nil && g.level >= gormlogger.Error:
		if errors.Is(err, gorm.ErrRecordNotFound) && g.ignoreRecordNotFoundErrors {
			return
		}
		g.withContext(ctx).Error("gorm query failed", append(fields, "error", err)...)
	case g.slowThreshold > 0 && elapsed > g.slowThreshold && g.level >= gormlogger.Warn:
		g.withContext(ctx).Warn("gorm slow query",
			append(fields, "slow_threshold_ms", g.slowThreshold.Milliseconds())...,
		)
	case g.level >= gormlogger.Info:
		g.withContext(ctx).Debug("gorm query", fields...)
	}
}

func (g *GORMLogger) withContext(ctx context.Context) *Logger {
	if ctx == nil {
		return g.log
	}
	return FromContext(ctx).Named("gorm")
}

func (g *GORMLogger) formatSQL(sql string) string {
	normalized := strings.Join(strings.Fields(sql), " ")
	if normalized == "" {
		return normalized
	}

	if g.includeQuery {
		if len(normalized) > 512 {
			return normalized[:512]
		}
		return normalized
	}

	redacted := sqlStringPattern.ReplaceAllString(normalized, "'?'")
	redacted = sqlNumberPattern.ReplaceAllString(redacted, "?")
	if len(redacted) > 512 {
		return redacted[:512]
	}
	return redacted
}

func parseGORMLevel(level string) gormlogger.LogLevel {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "silent":
		return gormlogger.Silent
	case "error":
		return gormlogger.Error
	case "warn":
		return gormlogger.Warn
	case "info", "debug":
		return gormlogger.Info
	default:
		return gormlogger.Warn
	}
}

func sanitizeArgs(args []interface{}) []interface{} {
	sanitized := make([]interface{}, 0, len(args))
	for _, arg := range args {
		if arg == nil {
			sanitized = append(sanitized, arg)
			continue
		}
		switch v := arg.(type) {
		case string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool, time.Time:
			sanitized = append(sanitized, v)
		case []byte:
			sanitized = append(sanitized, string(v))
		default:
			sanitized = append(sanitized, fmt.Sprintf("[unserializable:%T]", v))
		}
	}
	return sanitized
}
