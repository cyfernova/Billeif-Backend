package logger

import (
	"io"
	"os"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestIsProductionEnvironment(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want bool
	}{
		{name: "prod", env: "prod", want: true},
		{name: "production", env: "production", want: true},
		{name: "uppercase", env: "PRODUCTION", want: true},
		{name: "dev", env: "dev", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsProductionEnvironment(tc.env); got != tc.want {
				t.Fatalf("IsProductionEnvironment(%q) = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

func TestLoggerRedactsSensitiveFields(t *testing.T) {
	core, observed := observer.New(zapcore.DebugLevel)
	log := FromZap(zap.New(core))

	log.Info("redaction test",
		"password", "plain",
		"token", "abc",
		"authorization", "Bearer secret",
		"user_id", "user-1",
	)

	entries := observed.AllUntimed()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}

	fields := entries[0].ContextMap()
	if fields["password"] != redactedValue {
		t.Fatalf("password should be redacted, got %v", fields["password"])
	}
	if fields["token"] != redactedValue {
		t.Fatalf("token should be redacted, got %v", fields["token"])
	}
	if fields["authorization"] != redactedValue {
		t.Fatalf("authorization should be redacted, got %v", fields["authorization"])
	}
	if fields["user_id"] != "user-1" {
		t.Fatalf("user_id should not be redacted, got %v", fields["user_id"])
	}
}

func TestNewWithConfigIncludesCallerMetadata(t *testing.T) {
	output := captureStderr(func() {
		log := NewWithConfig(Config{
			Environment: "dev",
			Level:       "debug",
			Format:      "json",
		})
		log.Info("caller metadata test")
		log.Sync()
	})

	if !strings.Contains(output, "\"caller\":") && !strings.Contains(output, "\"C\":") {
		t.Fatalf("expected caller metadata in log output, got: %s", output)
	}
}

func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() {
		_ = w.Close()
		os.Stderr = old
	}()

	fn()
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}
