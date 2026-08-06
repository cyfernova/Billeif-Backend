package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"invoice-backend/internal/voice/loadtest"
)

func TestRunDefaultsToDeterministicFakeDryRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(nil, &stdout, &stderr, func(string) string { return "" }); err != nil {
		t.Fatalf("run() error = %v; stderr=%q", err, stderr.String())
	}
	var report loadtest.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v: %q", err, stdout.String())
	}
	if report.Mode != loadtest.ModeDeterministicFake || !report.DryRun || report.Metrics.SessionsAttempted != 120 {
		t.Fatalf("default report = %#v", report)
	}
	if report.ProductionPassCriteriaStatus != loadtest.StatusNotExecuted || report.Cost != (loadtest.CostFields{}) {
		t.Fatalf("default command fabricated live evidence: %#v", report)
	}
}

func TestRunRefusesNonDryFakeMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-dry-run=false"}, &stdout, &stderr, func(string) string { return "" })
	if !errors.Is(err, loadtest.ErrInvalidDryRun) {
		t.Fatalf("run() error = %v, want ErrInvalidDryRun", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("refused command wrote output: %q", stdout.String())
	}
}
