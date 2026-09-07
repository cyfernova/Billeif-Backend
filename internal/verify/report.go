package verify

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Harness identity constants.
const (
	SchemaVersion  = 1
	HarnessName    = "billeif-staging-verification-harness"
	HarnessVersion = "1"
)

// Run modes. Read mode only executes read-only checks; write mode additionally
// executes externally visible checks and requires a second deliberate flag.
const (
	ModeRead  = "read"
	ModeWrite = "write"
)

// Check statuses.
type Status string

const (
	StatusPassed        Status = "passed"
	StatusFailed        Status = "failed"
	StatusSkipped       Status = "skipped"
	StatusBlocked       Status = "blocked"
	StatusNotConfigured Status = "not_configured"
)

// Impact classes separate read-only checks from externally visible ones.
type Impact string

const (
	ImpactReadOnly      Impact = "read_only"
	ImpactExternalWrite Impact = "external_write"
)

// Cleanup statuses.
type CleanupStatus string

const (
	CleanupNotApplicable CleanupStatus = "not_applicable"
	CleanupSucceeded     CleanupStatus = "succeeded"
	CleanupFailed        CleanupStatus = "failed"
	CleanupPreserved     CleanupStatus = "preserved"
)

// Stable blocking/skip reason codes.
const (
	ReasonWriteModeRequired     = "write_mode_required"
	ReasonDependencyNotVerified = "dependency_not_verified"
	ReasonRunContextCanceled    = "run_context_canceled"
)

// Report is the machine-readable harness output. Field order and shape are
// deterministic; consumers should rely on the JSON keys documented here.
type Report struct {
	SchemaVersion           int            `json:"schema_version"`
	RunID                   string         `json:"run_id"`
	Harness                 HarnessInfo    `json:"harness"`
	Environment             string         `json:"environment"`
	Mode                    string         `json:"mode"`
	AllowProductionOverride bool           `json:"allow_production_override"`
	StartedAt               TimestampValue `json:"started_at"`
	FinishedAt              TimestampValue `json:"finished_at"`
	DurationMS              int64          `json:"duration_ms"`
	Selection               SelectionInfo  `json:"selection"`
	Aggregate               Aggregate      `json:"aggregate"`
	Checks                  []CheckResult  `json:"checks"`
}

// HarnessInfo identifies the producing binary.
type HarnessInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// SelectionInfo records how the run was scoped.
type SelectionInfo struct {
	Checks       string   `json:"checks"`
	Only         []string `json:"only"`
	Exclude      []string `json:"exclude"`
	CheckTimeout string   `json:"check_timeout"`
	MaxAttempts  int      `json:"max_attempts"`
	Concurrency  int      `json:"concurrency"`
}

// Aggregate summarizes the run.
type Aggregate struct {
	Status string         `json:"status"`
	Counts map[string]int `json:"counts"`
	Total  int            `json:"total"`
}

// CheckResult is one check's outcome.
type CheckResult struct {
	ID           string         `json:"id"`
	Provider     string         `json:"provider"`
	Description  string         `json:"description"`
	Impact       Impact         `json:"impact"`
	Dependencies []string       `json:"dependencies"`
	Status       Status         `json:"status"`
	StartedAt    TimestampValue `json:"started_at"`
	FinishedAt   TimestampValue `json:"finished_at"`
	DurationMS   int64          `json:"duration_ms"`
	Attempts     int            `json:"attempts"`
	Error        *ErrorDetail   `json:"error,omitempty"`
	Evidence     []Evidence     `json:"evidence"`
	Cleanup      CleanupRecord  `json:"cleanup"`
}

// ErrorDetail carries a sanitized error. Message is bounded and redacted.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Evidence is one bounded, redacted diagnostic key/value pair.
type Evidence struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// ResourceRecord describes a synthetic resource a write check created.
type ResourceRecord struct {
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
	Preserved bool   `json:"preserved"`
}

// CleanupRecord records synthetic-resource cleanup.
type CleanupRecord struct {
	Status        CleanupStatus    `json:"status"`
	Attempts      int              `json:"attempts"`
	Resources     []ResourceRecord `json:"resources"`
	DiagnosticRef string           `json:"diagnostic_ref,omitempty"`
	Error         string           `json:"error,omitempty"`
}

// TimestampValue serializes as an RFC3339 UTC string; the zero value
// serializes as an empty string to keep the schema deterministic.
type TimestampValue struct {
	time.Time
}

// Timestamp builds a UTC RFC3339 timestamp value.
func Timestamp(t time.Time) TimestampValue {
	return TimestampValue{Time: t.UTC()}
}

func (t TimestampValue) MarshalJSON() ([]byte, error) {
	if t.Time.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(t.Time.UTC().Format(time.RFC3339Nano))
}

func (t *TimestampValue) UnmarshalJSON(raw []byte) error {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	if s == "" {
		t.Time = time.Time{}
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	t.Time = parsed.UTC()
	return nil
}

// ComputeAggregate derives the aggregate status. Precedence: failed > blocked
// > not_configured > passed > skipped. Optional checks must be explicitly
// excluded; silently treating missing configuration as an overall pass would
// overstate release evidence.
func ComputeAggregate(statuses []Status) Aggregate {
	counts := map[string]int{
		string(StatusPassed):        0,
		string(StatusFailed):        0,
		string(StatusSkipped):       0,
		string(StatusBlocked):       0,
		string(StatusNotConfigured): 0,
	}
	for _, status := range statuses {
		counts[string(status)]++
	}
	total := len(statuses)
	status := string(StatusPassed)
	switch {
	case total == 0:
		status = string(StatusSkipped)
	case counts[string(StatusFailed)] > 0:
		status = string(StatusFailed)
	case counts[string(StatusBlocked)] > 0:
		status = string(StatusBlocked)
	case counts[string(StatusNotConfigured)] > 0:
		status = string(StatusNotConfigured)
	case counts[string(StatusPassed)] == 0:
		status = string(StatusSkipped)
	}
	return Aggregate{Status: status, Counts: counts, Total: total}
}

// sortCheckResults orders checks by ID for deterministic output.
func sortCheckResults(results []CheckResult) {
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
}
