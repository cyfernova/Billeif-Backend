package logger

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGORMLoggerFormatsNonJSONArgs(t *testing.T) {
	output := captureStderr(func() {
		log := NewWithConfig(Config{
			Environment:     "dev",
			Level:           "debug",
			Format:          "json",
			StacktraceLevel: "fatal",
		})
		gormLog := NewGORMLogger(log, GORMOptions{Level: "info"})

		gormLog.Error(context.Background(), "callback %v", func() time.Time { return time.Now() })
		log.Sync()
	})

	if strings.Contains(output, "unsupported type") {
		t.Fatalf("gorm logger should not emit JSON encoding errors, got: %s", output)
	}
	if strings.Contains(output, "argsError") {
		t.Fatalf("gorm logger should not log raw variadic args, got: %s", output)
	}
	if !strings.Contains(output, "callback <func() time.Time>") {
		t.Fatalf("expected formatted callback message, got: %s", output)
	}
}
