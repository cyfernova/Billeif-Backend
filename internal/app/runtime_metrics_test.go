package app

import (
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/operationsmetrics"
)

func TestInitOperationsMetricsEmitterFailsOpen(t *testing.T) {
	if emitter := initOperationsMetricsEmitter(&config.Config{Environment: ""}); emitter != nil {
		t.Fatalf("missing environment must disable emission, got %#v", emitter)
	}
	if emitter := initOperationsMetricsEmitter(&config.Config{Environment: "invalid environment"}); emitter != nil {
		t.Fatalf("unsafe environment must disable emission, got %#v", emitter)
	}
	emitter := initOperationsMetricsEmitter(&config.Config{Environment: "production"})
	if emitter == nil {
		t.Fatalf("a valid environment must produce an emitter")
	}
	var nilEmitter *operationsmetrics.Emitter
	if emitter == nilEmitter {
		t.Fatalf("emitter identity mismatch")
	}
}
