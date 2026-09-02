package verify

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReportSchemaIsDeterministic(t *testing.T) {
	fixed := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	build := func() *Report {
		return &Report{
			SchemaVersion:           1,
			RunID:                   "fixed-run-id",
			Environment:             "staging",
			Mode:                    ModeRead,
			AllowProductionOverride: false,
			StartedAt:               Timestamp(fixed),
			FinishedAt:              Timestamp(fixed.Add(2 * time.Second)),
			DurationMS:              2000,
			Selection: SelectionInfo{
				Checks:  ModeRead,
				Only:    []string{"postgres.connect"},
				Exclude: []string{},
			},
			Aggregate: Aggregate{Status: "passed", Counts: ComputeAggregate([]Status{StatusPassed}).Counts, Total: 1},
			Checks: []CheckResult{{
				ID:           "postgres.connect",
				Provider:     "postgresql",
				Description:  "PostgreSQL connectivity",
				Impact:       ImpactReadOnly,
				Dependencies: []string{},
				Status:       StatusPassed,
				StartedAt:    Timestamp(fixed),
				FinishedAt:   Timestamp(fixed.Add(time.Second)),
				DurationMS:   1000,
				Attempts:     1,
				Evidence: []Evidence{
					{Key: "connected", Value: true},
					{Key: "server_version", Value: "PostgreSQL 16.2"},
				},
				Cleanup: CleanupRecord{Status: CleanupNotApplicable, Attempts: 0, Resources: []ResourceRecord{}},
			}},
		}
	}
	first, err := json.Marshal(build())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	second, err := json.Marshal(build())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("schema must be deterministic:\nfirst=%s\nsecond=%s", first, second)
	}
	var decoded map[string]any
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{
		"schema_version", "run_id", "environment", "mode", "allow_production_override",
		"started_at", "finished_at", "duration_ms", "selection", "aggregate", "checks",
	} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("report schema missing key %q in %s", key, first)
		}
	}
	check := decoded["checks"].([]any)[0].(map[string]any)
	for _, key := range []string{
		"id", "provider", "description", "impact", "dependencies", "status",
		"started_at", "finished_at", "duration_ms", "attempts", "evidence", "cleanup",
	} {
		if _, ok := check[key]; !ok {
			t.Fatalf("check schema missing key %q in %s", key, first)
		}
	}
}

func TestTimestampsAreUTCAndRFC3339(t *testing.T) {
	stamp := Timestamp(time.Date(2026, 9, 3, 10, 0, 0, 123456789, time.FixedZone("IST", 5*3600+1800)))
	raw, err := stamp.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal timestamp: %v", err)
	}
	value := string(raw)
	value = strings.Trim(value, `"`)
	if !strings.HasSuffix(value, "Z") {
		t.Fatalf("timestamp %q must be UTC (Z suffix)", value)
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("timestamp %q is not RFC3339: %v", value, err)
	}
	if parsed.UTC().Hour() != 4 {
		t.Fatalf("timestamp %q did not normalize the timezone to UTC", value)
	}
}

func TestTimestampsAreEmptyWhenZero(t *testing.T) {
	var zero TimestampValue
	if raw, err := json.Marshal(zero); err != nil || string(raw) != `""` {
		t.Fatalf("zero timestamp must marshal as empty string, got %s err=%v", raw, err)
	}
}

func TestAggregateComputationCoversEveryStatus(t *testing.T) {
	cases := []struct {
		name    string
		status  []Status
		wantAgg string
	}{
		{name: "all passed", status: []Status{StatusPassed, StatusPassed}, wantAgg: "passed"},
		{name: "any failed wins", status: []Status{StatusPassed, StatusFailed}, wantAgg: "failed"},
		{name: "blocked when no failure", status: []Status{StatusPassed, StatusBlocked}, wantAgg: "blocked"},
		{name: "not configured when nothing verified", status: []Status{StatusNotConfigured, StatusNotConfigured}, wantAgg: "not_configured"},
		{name: "not configured alongside passed", status: []Status{StatusPassed, StatusNotConfigured}, wantAgg: "not_configured"},
		{name: "all skipped", status: []Status{StatusSkipped, StatusSkipped}, wantAgg: "skipped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agg := ComputeAggregate(tc.status)
			if agg.Status != tc.wantAgg {
				t.Fatalf("aggregate status = %q, want %q (counts=%v)", agg.Status, tc.wantAgg, agg.Counts)
			}
			total := 0
			for _, count := range agg.Counts {
				total += count
			}
			if total != len(tc.status) {
				t.Fatalf("counts total = %d, want %d", total, len(tc.status))
			}
			if agg.Total != len(tc.status) {
				t.Fatalf("aggregate total = %d, want %d", agg.Total, len(tc.status))
			}
		})
	}
}

func TestAllStatusesAndImpactsHaveStableSerializations(t *testing.T) {
	for _, status := range []Status{StatusPassed, StatusFailed, StatusSkipped, StatusBlocked, StatusNotConfigured} {
		raw, err := json.Marshal(status)
		if err != nil {
			t.Fatalf("marshal status %q: %v", status, err)
		}
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("status must serialize as string: %v", err)
		}
		if decoded != string(status) {
			t.Fatalf("status serialization %q does not match %q", decoded, status)
		}
	}
	for _, impact := range []Impact{ImpactReadOnly, ImpactExternalWrite} {
		raw, _ := json.Marshal(impact)
		var decoded string
		_ = json.Unmarshal(raw, &decoded)
		if decoded != string(impact) {
			t.Fatalf("impact serialization %q does not match %q", decoded, impact)
		}
	}
}
