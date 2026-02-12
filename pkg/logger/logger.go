package logger

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger struct {
	*zap.SugaredLogger
}

type Config struct {
	Environment        string
	Level              string
	Format             string
	SamplingInitial    int
	SamplingThereafter int
	StacktraceLevel    string
}

var globalLogger *Logger
var globalMu sync.RWMutex

const redactedValue = "[REDACTED]"

var sensitiveKeys = []string{
	"password",
	"token",
	"secret",
	"authorization",
	"api_key",
	"access_key",
	"refresh_token",
	"cookie",
}

func New() *Logger {
	return NewWithConfig(Config{
		Environment:        os.Getenv("ENVIRONMENT"),
		Level:              os.Getenv("LOG_LEVEL"),
		Format:             os.Getenv("LOG_FORMAT"),
		SamplingInitial:    parseIntEnv("LOG_SAMPLING_INITIAL", 0),
		SamplingThereafter: parseIntEnv("LOG_SAMPLING_THEREAFTER", 0),
		StacktraceLevel:    os.Getenv("LOG_STACKTRACE_LEVEL"),
	})
}

func NewWithEnv(env string) *Logger {
	return NewWithConfig(Config{Environment: env})
}

func NewWithConfig(cfg Config) *Logger {
	cfg = normalizeConfig(cfg)

	var zapCfg zap.Config
	if IsProductionEnvironment(cfg.Environment) {
		zapCfg = zap.NewProductionConfig()
	} else {
		zapCfg = zap.NewDevelopmentConfig()
	}

	zapCfg.Level = zap.NewAtomicLevelAt(parseLevel(cfg.Level))
	zapCfg.Encoding = cfg.Format
	zapCfg.EncoderConfig.TimeKey = "timestamp"
	zapCfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	if cfg.Format == "console" && !IsProductionEnvironment(cfg.Environment) {
		zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	if cfg.SamplingInitial > 0 && cfg.SamplingThereafter > 0 {
		zapCfg.Sampling = &zap.SamplingConfig{
			Initial:    cfg.SamplingInitial,
			Thereafter: cfg.SamplingThereafter,
		}
	} else {
		zapCfg.Sampling = nil
	}

	zapLogger, err := zapCfg.Build(
		zap.AddCaller(),
		zap.AddCallerSkip(1),
		zap.AddStacktrace(parseStacktraceLevel(cfg.StacktraceLevel)),
	)
	if err != nil {
		zapLogger = zap.NewNop()
	}

	return &Logger{SugaredLogger: zapLogger.Sugar()}
}

func FromZap(zapLogger *zap.Logger) *Logger {
	if zapLogger == nil {
		zapLogger = zap.NewNop()
	}
	return &Logger{SugaredLogger: zapLogger.Sugar()}
}

func normalizeConfig(cfg Config) Config {
	if cfg.Environment == "" {
		cfg.Environment = "dev"
	}
	if cfg.Level == "" {
		if IsProductionEnvironment(cfg.Environment) {
			cfg.Level = "info"
		} else {
			cfg.Level = "debug"
		}
	}
	if cfg.Format == "" {
		if IsProductionEnvironment(cfg.Environment) {
			cfg.Format = "json"
		} else {
			cfg.Format = "console"
		}
	}
	if cfg.StacktraceLevel == "" {
		cfg.StacktraceLevel = "error"
	}
	if cfg.SamplingInitial == 0 && cfg.SamplingThereafter == 0 && IsProductionEnvironment(cfg.Environment) {
		cfg.SamplingInitial = 100
		cfg.SamplingThereafter = 100
	}
	return cfg
}

func IsProductionEnvironment(env string) bool {
	normalized := strings.ToLower(strings.TrimSpace(env))
	return normalized == "prod" || normalized == "production"
}

func Init(env string) error {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalLogger = NewWithEnv(env)
	return nil
}

func InitWithConfig(cfg Config) error {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalLogger = NewWithConfig(cfg)
	return nil
}

func SetGlobal(log *Logger) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalLogger = log
}

func Global() *Logger {
	globalMu.RLock()
	if globalLogger != nil {
		defer globalMu.RUnlock()
		return globalLogger
	}
	globalMu.RUnlock()

	globalMu.Lock()
	defer globalMu.Unlock()
	if globalLogger == nil {
		globalLogger = New()
	}
	return globalLogger
}

func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Infow(msg, sanitizeKeyValues(keysAndValues...)...)
}

func (l *Logger) Debug(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Debugw(msg, sanitizeKeyValues(keysAndValues...)...)
}

func (l *Logger) Warn(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Warnw(msg, sanitizeKeyValues(keysAndValues...)...)
}

func (l *Logger) Error(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Errorw(msg, sanitizeKeyValues(keysAndValues...)...)
}

func (l *Logger) Fatal(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Fatalw(msg, sanitizeKeyValues(keysAndValues...)...)
}

func (l *Logger) With(keysAndValues ...interface{}) *Logger {
	return &Logger{SugaredLogger: l.SugaredLogger.With(sanitizeKeyValues(keysAndValues...)...)}
}

func (l *Logger) Named(name string) *Logger {
	return &Logger{SugaredLogger: l.SugaredLogger.Named(name)}
}

func (l *Logger) Sync() {
	_ = l.SugaredLogger.Sync()
}

func parseIntEnv(key string, fallback int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseLevel(level string) zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return zapcore.DebugLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

func parseStacktraceLevel(level string) zapcore.LevelEnabler {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "dpanic":
		return zapcore.DPanicLevel
	case "panic":
		return zapcore.PanicLevel
	case "fatal":
		return zapcore.FatalLevel
	default:
		return zapcore.ErrorLevel
	}
}

func sanitizeKeyValues(keysAndValues ...interface{}) []interface{} {
	if len(keysAndValues) == 0 {
		return keysAndValues
	}

	sanitized := make([]interface{}, 0, len(keysAndValues)+1)
	for i := 0; i < len(keysAndValues); i += 2 {
		key := keysAndValues[i]
		value := interface{}(nil)
		if i+1 < len(keysAndValues) {
			value = keysAndValues[i+1]
		}

		if isSensitiveKey(key) {
			value = redactedValue
		}

		sanitized = append(sanitized, normalizeKey(key), value)
	}

	if len(sanitized)%2 != 0 {
		sanitized = append(sanitized, "missing_value")
	}

	return sanitized
}

func normalizeKey(key interface{}) interface{} {
	if key == nil {
		return "key"
	}
	return key
}

func isSensitiveKey(key interface{}) bool {
	keyStr := strings.ToLower(fmt.Sprint(key))
	for _, sensitive := range sensitiveKeys {
		if strings.Contains(keyStr, sensitive) {
			return true
		}
	}
	return false
}
