package main

import (
	"errors"
	"sync"
	"testing"

	voicetelemetry "invoice-backend/internal/voice/telemetry"
)

func TestLoadRuntimeSettingsRequiresOnlyNonSecretVoiceIdentifiers(t *testing.T) {
	values := map[string]string{
		"ENVIRONMENT":                    "prod",
		"VOICE_SESSIONS_TABLE_NAME":      "billeif-prod-voice-sessions",
		"VOICE_SESSION_LEASE_INDEX_NAME": "gsi2",
		"AGENTCORE_RUNTIME_ARN":          "arn:aws:bedrock-agentcore:ap-south-1:123456789012:runtime/voice",
		"AGENTCORE_RUNTIME_QUALIFIER":    "PROD",
		"VOICE_RECONCILER_BATCH_SIZE":    "25",
	}
	settings, err := loadRuntimeSettings(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if settings.tableName != values["VOICE_SESSIONS_TABLE_NAME"] || settings.leaseIndexName != "gsi2" ||
		settings.agentRuntimeARN != values["AGENTCORE_RUNTIME_ARN"] || settings.agentRuntimeQualifier != "PROD" ||
		settings.batchSize != 25 || settings.environment != "prod" {
		t.Fatalf("wrong settings: %#v", settings)
	}
}

func TestLoadRuntimeSettingsRejectsMissingOrUnboundedValues(t *testing.T) {
	valid := map[string]string{
		"ENVIRONMENT":                    "prod",
		"VOICE_SESSIONS_TABLE_NAME":      "billeif-prod-voice-sessions",
		"VOICE_SESSION_LEASE_INDEX_NAME": "gsi2",
		"AGENTCORE_RUNTIME_ARN":          "arn:aws:bedrock-agentcore:ap-south-1:123456789012:runtime/voice",
		"AGENTCORE_RUNTIME_QUALIFIER":    "PROD",
		"VOICE_RECONCILER_BATCH_SIZE":    "10",
	}
	cases := []struct {
		name  string
		key   string
		value string
	}{
		{name: "missing table", key: "VOICE_SESSIONS_TABLE_NAME", value: ""},
		{name: "missing environment", key: "ENVIRONMENT", value: ""},
		{name: "unsafe environment", key: "ENVIRONMENT", value: "prod/tenant"},
		{name: "missing runtime", key: "AGENTCORE_RUNTIME_ARN", value: ""},
		{name: "missing qualifier", key: "AGENTCORE_RUNTIME_QUALIFIER", value: ""},
		{name: "zero batch", key: "VOICE_RECONCILER_BATCH_SIZE", value: "0"},
		{name: "oversized batch", key: "VOICE_RECONCILER_BATCH_SIZE", value: "101"},
		{name: "invalid batch", key: "VOICE_RECONCILER_BATCH_SIZE", value: "many"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			values := make(map[string]string, len(valid))
			for key, value := range valid {
				values[key] = value
			}
			values[tc.key] = tc.value
			_, err := loadRuntimeSettings(func(key string) string { return values[key] })
			if !errors.Is(err, errInvalidRuntimeSettings) {
				t.Fatalf("expected safe settings error, got %v", err)
			}
		})
	}
}

func TestReconcilerMetricsEmitsOnlyRecoveredLeakCountAndFailsOpen(t *testing.T) {
	recorder := &reconcilerSignalRecorder{}
	metrics, err := newReconcilerMetrics(recorder)
	if err != nil {
		t.Fatalf("newReconcilerMetrics() error = %v", err)
	}
	metrics.SessionLeaksRecovered(3)
	if signal, value := recorder.snapshot(); signal != voicetelemetry.SignalSessionLeaks || value != 3 {
		t.Fatalf("reconciler signal = %s/%d", signal, value)
	}
	if _, err := newReconcilerMetrics(nil); !errors.Is(err, errInvalidRuntimeSettings) {
		t.Fatalf("nil recorder error = %v", err)
	}
	panicMetrics, err := newReconcilerMetrics(panicReconcilerSignalRecorder{})
	if err != nil {
		t.Fatalf("newReconcilerMetrics(panic) error = %v", err)
	}
	panicMetrics.SessionLeaksRecovered(1)
}

type reconcilerSignalRecorder struct {
	mu     sync.Mutex
	signal voicetelemetry.Signal
	value  int64
}

func (recorder *reconcilerSignalRecorder) RecordSignal(signal voicetelemetry.Signal, value int64) error {
	recorder.mu.Lock()
	recorder.signal = signal
	recorder.value = value
	recorder.mu.Unlock()
	return nil
}

func (recorder *reconcilerSignalRecorder) snapshot() (voicetelemetry.Signal, int64) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.signal, recorder.value
}

type panicReconcilerSignalRecorder struct{}

func (panicReconcilerSignalRecorder) RecordSignal(voicetelemetry.Signal, int64) error {
	panic("metrics must fail open")
}
