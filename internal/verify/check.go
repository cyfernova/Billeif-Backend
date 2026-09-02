package verify

import (
	"context"
	"strings"
)

// Metadata describes a check in the report.
type Metadata struct {
	ID           string
	Provider     string
	Description  string
	Impact       Impact
	Dependencies []string
}

// Check is one verification probe. Probes must be safe to call concurrently,
// must honor the context deadline, and must never perform writes unless their
// metadata declares ImpactExternalWrite.
type Check interface {
	Metadata() Metadata
	Probe(ctx context.Context) Outcome
}

// Resource is a synthetic resource a write check created, using a sanitized
// reference (never a secret and never a raw provider URL with credentials).
type Resource struct {
	Kind      string
	Reference string
}

// CleanupFunc performs best-effort cleanup of synthetic resources. It must be
// idempotent and must honor its context.
type CleanupFunc func(ctx context.Context) ([]Evidence, error)

// Outcome is a probe's result. Status must be one of the five supported
// statuses; probes return not_configured when their prerequisites are absent
// rather than guessing, and blocked when a gating condition prevents
// execution (for example a live-mode credential in a staging run).
type Outcome struct {
	Status    Status
	Err       error
	ErrCode   string
	Retryable bool
	Evidence  []Evidence
	Resources []Resource
	Cleanup   CleanupFunc
}

// NotConfigured returns a truthful not_configured outcome for the given
// prerequisite list.
func NotConfigured(prerequisites ...string) Outcome {
	evidence := make([]Evidence, 0, len(prerequisites)+1)
	evidence = append(evidence, Evidence{Key: "reason", Value: "missing_configuration"})
	if len(prerequisites) > 0 {
		evidence = append(evidence, Evidence{
			Key:   "missing",
			Value: strings.Join(prerequisites, ","),
		})
	}
	return Outcome{Status: StatusNotConfigured, Evidence: evidence}
}

// Blocked returns a blocked outcome with a stable reason code.
func Blocked(reason string, evidence ...Evidence) Outcome {
	items := make([]Evidence, 0, len(evidence)+1)
	items = append(items, Evidence{Key: "reason", Value: reason})
	items = append(items, evidence...)
	return Outcome{Status: StatusBlocked, Evidence: items}
}

// Failed returns a failed outcome. Retryable marks transient failures so the
// runner can attempt bounded retries.
func Failed(err error, code string, retryable bool, evidence ...Evidence) Outcome {
	return Outcome{Status: StatusFailed, Err: err, ErrCode: code, Retryable: retryable, Evidence: evidence}
}

// EvidenceItem is a small helper for building evidence pairs.
func EvidenceItem(key string, value any) Evidence {
	return Evidence{Key: key, Value: value}
}
