package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stable refusal reasons enforced by the harness entry points.
const (
	ReasonWriteRequiresExplicitAllow = "write_mode_requires_explicit_allow_writes"
	ReasonInvalidMode                = "invalid_mode"
	ReasonUnknownCheck               = "unknown_check_id"
	ReasonEmptySelection             = "empty_selection"
	ReasonDependencyMissing          = "dependency_missing"
	ReasonDependencyCycle            = "dependency_cycle"
)

// Defaults honored by the runner unless overridden.
const (
	DefaultConcurrency  = 4
	MaximumConcurrency  = 16
	DefaultCheckTimeout = 30 * time.Second
	DefaultCleanupTime  = 60 * time.Second
	DefaultMaxAttempts  = 3
	MaximumAttempts     = 5
	DefaultBackoffBase  = 500 * time.Millisecond
	DefaultBackoffMax   = 8 * time.Second
)

// UsageRefusalError is a deterministic, operator-facing refusal that happens
// before any probe executes.
type UsageRefusalError struct {
	Reason string
	Detail string
}

func (e *UsageRefusalError) Error() string {
	if e.Detail == "" {
		return "verification refused: " + e.Reason
	}
	return fmt.Sprintf("verification refused (%s): %s", e.Reason, e.Detail)
}

// RunOptions configures a verification run. Environment and Mode are
// mandatory; AllowProductionOverride and AllowWrites are the two deliberate
// override flags.
type RunOptions struct {
	RunID     string
	StartedAt time.Time

	Environment             string
	AllowProductionOverride bool

	Mode        string
	AllowWrites bool

	Only    []string
	Exclude []string

	Concurrency   int
	CheckTimeout  time.Duration
	CleanupTimout time.Duration
	MaxAttempts   int
	BackoffBase   time.Duration
	BackoffMax    time.Duration

	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

func (o *RunOptions) normalize() error {
	if o.Concurrency <= 0 {
		o.Concurrency = DefaultConcurrency
	}
	if o.Concurrency > MaximumConcurrency {
		o.Concurrency = MaximumConcurrency
	}
	if o.CheckTimeout <= 0 {
		o.CheckTimeout = DefaultCheckTimeout
	}
	if o.CleanupTimout <= 0 {
		o.CleanupTimout = DefaultCleanupTime
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = DefaultMaxAttempts
	}
	if o.MaxAttempts > MaximumAttempts {
		o.MaxAttempts = MaximumAttempts
	}
	if o.BackoffBase <= 0 {
		o.BackoffBase = DefaultBackoffBase
	}
	if o.BackoffMax < o.BackoffBase {
		o.BackoffMax = DefaultBackoffMax
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Sleep == nil {
		o.Sleep = func(ctx context.Context, d time.Duration) error {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	if o.Mode != ModeRead && o.Mode != ModeWrite {
		return &UsageRefusalError{Reason: ReasonInvalidMode, Detail: fmt.Sprintf("mode %q must be %q or %q", o.Mode, ModeRead, ModeWrite)}
	}
	return nil
}

// RunHarness validates the request, selects checks, executes them with bounded
// concurrency/retries, and returns the redacted deterministic report.
func RunHarness(ctx context.Context, options RunOptions, checks []Check) (*Report, error) {
	if err := options.normalize(); err != nil {
		return nil, err
	}
	environment, err := SelectEnvironment(options.Environment, options.AllowProductionOverride)
	if err != nil {
		return nil, err
	}
	if options.Mode == ModeWrite && !options.AllowWrites {
		return nil, &UsageRefusalError{
			Reason: ReasonWriteRequiresExplicitAllow,
			Detail: "externally visible checks require both --mode write and --allow-writes",
		}
	}
	selected, err := selectChecks(checks, options.Only, options.Exclude)
	if err != nil {
		return nil, err
	}
	startedAt := options.StartedAt
	if startedAt.IsZero() {
		startedAt = options.Now().UTC()
	}

	opts := options
	opts.Environment = environment.String()
	results, execErr := executeChecks(ctx, opts, selected)
	finishedAt := options.Now().UTC()
	if execErr != nil {
		return nil, err
	}

	runID := options.RunID
	if runID == "" {
		runID = "unspecified"
	}
	statuses := make([]Status, 0, len(results))
	for _, result := range results {
		statuses = append(statuses, result.Status)
	}
	report := &Report{
		SchemaVersion:           SchemaVersion,
		RunID:                   runID,
		Harness:                 HarnessInfo{Name: HarnessName},
		Environment:             environment.String(),
		Mode:                    options.Mode,
		AllowProductionOverride: options.AllowProductionOverride && environment.IsProduction(),
		StartedAt:               Timestamp(startedAt),
		FinishedAt:              Timestamp(finishedAt),
		DurationMS:              finishedAt.Sub(startedAt).Milliseconds(),
		Selection: SelectionInfo{
			Checks:       options.Mode,
			Only:         options.Only,
			Exclude:      options.Exclude,
			CheckTimeout: options.CheckTimeout.String(),
			MaxAttempts:  options.MaxAttempts,
			Concurrency:  options.Concurrency,
		},
		Aggregate: ComputeAggregate(statuses),
		Checks:    results,
	}
	return redactReport(report), nil
}

// selectChecks applies Only (with dependency closure) and Exclude, validating
// identifiers and rejecting an empty selection.
func selectChecks(checks []Check, only, exclude []string) ([]Check, error) {
	byID := make(map[string]Check, len(checks))
	for _, check := range checks {
		meta := check.Metadata()
		if meta.ID == "" {
			return nil, &UsageRefusalError{Reason: ReasonUnknownCheck, Detail: "every check must declare an ID"}
		}
		if _, duplicate := byID[meta.ID]; duplicate {
			return nil, &UsageRefusalError{Reason: ReasonUnknownCheck, Detail: "duplicate check ID " + meta.ID}
		}
		byID[meta.ID] = check
	}

	excluded := make(map[string]struct{}, len(exclude))
	for _, id := range exclude {
		if _, ok := byID[id]; !ok {
			return nil, &UsageRefusalError{Reason: ReasonUnknownCheck, Detail: "--exclude id " + id}
		}
		excluded[id] = struct{}{}
	}

	selected := make(map[string]struct{}, len(checks))
	var addWithDeps func(id string) error
	addWithDeps = func(id string) error {
		if _, ok := byID[id]; !ok {
			return &UsageRefusalError{Reason: ReasonUnknownCheck, Detail: "check id " + id}
		}
		if _, done := selected[id]; done {
			return nil
		}
		selected[id] = struct{}{}
		for _, dep := range byID[id].Metadata().Dependencies {
			if _, ok := byID[dep]; !ok {
				return &UsageRefusalError{Reason: ReasonDependencyMissing, Detail: id + " depends on " + dep}
			}
			if _, skip := excluded[dep]; skip {
				return &UsageRefusalError{Reason: ReasonDependencyMissing, Detail: id + " depends on excluded " + dep}
			}
			if err := addWithDeps(dep); err != nil {
				return err
			}
		}
		return nil
	}
	if len(only) > 0 {
		for _, id := range only {
			if err := addWithDeps(id); err != nil {
				return nil, err
			}
		}
	} else {
		for _, id := range byID {
			if _, skip := excluded[id.Metadata().ID]; skip {
				continue
			}
			if err := addWithDeps(id.Metadata().ID); err != nil {
				return nil, err
			}
		}
	}
	if len(selected) == 0 {
		return nil, &UsageRefusalError{Reason: ReasonEmptySelection, Detail: "no checks selected"}
	}
	out := make([]Check, 0, len(selected))
	for _, check := range checks {
		if _, ok := selected[check.Metadata().ID]; ok {
			out = append(out, check)
		}
	}
	return out, nil
}

type completion struct {
	id     string
	result CheckResult
}

// executeChecks runs checks with dependency awareness, bounded global
// concurrency, per-check deadlines, bounded retries with backoff, and cleanup
// bookkeeping.
func executeChecks(ctx context.Context, options RunOptions, checks []Check) ([]CheckResult, error) {
	if err := options.normalize(); err != nil {
		return nil, err
	}
	if options.Mode == ModeWrite && !options.AllowWrites {
		return nil, &UsageRefusalError{
			Reason: ReasonWriteRequiresExplicitAllow,
			Detail: "externally visible checks require both mode=write and the allow-writes flag",
		}
	}
	byID := make(map[string]Check, len(checks))
	dependents := make(map[string][]string, len(checks))
	remainingDeps := make(map[string]int, len(checks))
	for _, check := range checks {
		meta := check.Metadata()
		if _, duplicate := byID[meta.ID]; duplicate {
			return nil, &UsageRefusalError{Reason: ReasonUnknownCheck, Detail: "duplicate check ID " + meta.ID}
		}
		byID[meta.ID] = check
	}
	for id, check := range byID {
		deps := check.Metadata().Dependencies
		remainingDeps[id] = len(deps)
		for _, dep := range deps {
			if _, ok := byID[dep]; !ok {
				return nil, &UsageRefusalError{Reason: ReasonDependencyMissing, Detail: id + " depends on unknown " + dep}
			}
			dependents[dep] = append(dependents[dep], id)
		}
	}
	if err := ensureAcyclic(byID); err != nil {
		return nil, err
	}

	statuses := make(map[string]Status, len(checks))
	results := make([]CheckResult, 0, len(checks))
	completions := make(chan completion, len(checks))
	running := 0
	sem := make(chan struct{}, options.Concurrency)

	launch := func(id string) {
		check := byID[id]
		running++
		go func() {
			// Acquire a bounded slot before executing so the global concurrency
			// cap is never exceeded; cancel or completion releases it.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				completions <- completion{id: id, result: blockedCanceledResult(metaByID(byID, id), ctx)}
				return
			}
			result := runSingleCheck(ctx, options, check)
			<-sem
			completions <- completion{id: id, result: result}
		}()
	}

	// evaluateReadiness launches every check whose dependencies all passed and
	// records blocked/skipped results for checks whose dependencies finished
	// but cannot support execution.
	evaluateReadiness := func() {
		for id, count := range remainingDeps {
			if count != 0 {
				continue
			}
			if _, terminal := statuses[id]; terminal {
				continue
			}
			blockedResult, hasBlocker := blockForDependencies(byID[id], statuses, ctx)
			if hasBlocker {
				statuses[id] = blockedResult.Status
				results = append(results, blockedResult)
				continue
			}
			launch(id)
		}
	}

	evaluateReadiness()

	for len(results) < len(checks) {
		if running == 0 {
			// Nothing is running and checks are still pending: they are
			// blocked by terminal dependencies that did not pass.
			progressed := false
			for id := range remainingDeps {
				if _, terminal := statuses[id]; terminal {
					continue
				}
				blockedResult, ok := blockForDependencies(byID[id], statuses, ctx)
				if !ok {
					return nil, &UsageRefusalError{Reason: ReasonDependencyCycle, Detail: "no progress possible for " + id}
				}
				statuses[id] = blockedResult.Status
				results = append(results, blockedResult)
				progressed = true
			}
			if !progressed {
				return nil, &UsageRefusalError{Reason: ReasonDependencyCycle, Detail: "scheduler stalled"}
			}
			continue
		}
		var done completion
		select {
		case <-ctx.Done():
			// Mark everything non-terminal blocked so the run finishes while
			// in-flight probes wind down on their own deadline contexts.
			for id := range remainingDeps {
				if _, terminal := statuses[id]; terminal {
					continue
				}
				meta := byID[id].Metadata()
				statuses[id] = StatusBlocked
				results = append(results, CheckResult{
					ID:           id,
					Provider:     meta.Provider,
					Description:  meta.Description,
					Impact:       meta.Impact,
					Dependencies: meta.Dependencies,
					Status:       StatusBlocked,
					Evidence:     []Evidence{{Key: "reason", Value: ReasonRunContextCanceled}},
					Cleanup:      CleanupRecord{Status: CleanupNotApplicable, Resources: []ResourceRecord{}},
				})
			}
			continue
		case done = <-completions:
		}
		running--
		statuses[done.id] = done.result.Status
		results = append(results, done.result)
		for _, dependent := range dependents[done.id] {
			remainingDeps[dependent]--
			evaluateReadiness()
		}
	}

	sortCheckResults(results)
	return results, nil
}

// blockForDependencies returns a blocked/skipped result when every dependency
// is terminal and at least one of them prevents execution. It returns false
// when the check has no dependencies or when they all passed.
func blockForDependencies(check Check, statuses map[string]Status, ctx context.Context) (CheckResult, bool) {
	meta := check.Metadata()
	if len(meta.Dependencies) == 0 {
		return CheckResult{}, false
	}
	status := StatusPassed
	blockedBy := make([]string, 0, len(meta.Dependencies))
	for _, dep := range meta.Dependencies {
		depStatus, terminal := statuses[dep]
		if !terminal {
			return CheckResult{}, false
		}
		blockedBy = append(blockedBy, dep+":"+string(depStatus))
		switch depStatus {
		case StatusSkipped:
			if status == StatusPassed {
				status = StatusSkipped
			}
		case StatusFailed, StatusBlocked, StatusNotConfigured:
			status = StatusBlocked
		}
	}
	if status == StatusPassed {
		return CheckResult{}, false
	}
	reason := ReasonDependencyNotVerified
	if status == StatusSkipped {
		reason = ReasonWriteModeRequired
	}
	evidence := []Evidence{
		{Key: "reason", Value: reason},
		{Key: "blocked_by", Value: strings.Join(blockedBy, ",")},
	}
	if ctx.Err() != nil {
		evidence = append(evidence, Evidence{Key: "note", Value: ReasonRunContextCanceled})
	}
	return CheckResult{
		ID:           meta.ID,
		Provider:     meta.Provider,
		Description:  meta.Description,
		Impact:       meta.Impact,
		Dependencies: meta.Dependencies,
		Status:       status,
		Evidence:     evidence,
		Cleanup:      CleanupRecord{Status: CleanupNotApplicable, Resources: []ResourceRecord{}},
	}, true
}

// blockedCanceledResult builds the cancellation result for a check that was
// queued behind the concurrency bound when the run was canceled.
func blockedCanceledResult(meta Metadata, ctx context.Context) CheckResult {
	evidence := []Evidence{{Key: "reason", Value: ReasonRunContextCanceled}}
	if ctx.Err() != nil {
		evidence = append(evidence, Evidence{Key: "detail", Value: ctx.Err().Error()})
	}
	return CheckResult{
		ID:           meta.ID,
		Provider:     meta.Provider,
		Description:  meta.Description,
		Impact:       meta.Impact,
		Dependencies: meta.Dependencies,
		Status:       StatusBlocked,
		Evidence:     evidence,
		Cleanup:      CleanupRecord{Status: CleanupNotApplicable, Resources: []ResourceRecord{}},
	}
}

func metaByID(byID map[string]Check, id string) Metadata {
	if check, ok := byID[id]; ok {
		return check.Metadata()
	}
	return Metadata{ID: id}
}

func ensureAcyclic(byID map[string]Check) error {
	state := make(map[string]int, len(byID))
	var visit func(id string) error
	visit = func(id string) error {
		switch state[id] {
		case 1:
			return &UsageRefusalError{Reason: ReasonDependencyCycle, Detail: "cycle at " + id}
		case 2:
			return nil
		}
		state[id] = 1
		for _, dep := range byID[id].Metadata().Dependencies {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	for id := range byID {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

// runSingleCheck executes one check: mode gate, deadline, bounded retries with
// backoff, panic recovery, and cleanup bookkeeping.
func runSingleCheck(ctx context.Context, options RunOptions, check Check) CheckResult {
	meta := check.Metadata()
	result := CheckResult{
		ID:           meta.ID,
		Provider:     meta.Provider,
		Description:  meta.Description,
		Impact:       meta.Impact,
		Dependencies: meta.Dependencies,
		Cleanup:      CleanupRecord{Status: CleanupNotApplicable, Resources: []ResourceRecord{}},
	}
	if meta.Impact == ImpactExternalWrite && !(options.Mode == ModeWrite && options.AllowWrites) {
		result.Status = StatusSkipped
		result.Evidence = []Evidence{{Key: "reason", Value: ReasonWriteModeRequired}}
		return result
	}

	started := options.Now().UTC()
	result.StartedAt = Timestamp(started)
	var outcome Outcome
	for attempt := 1; attempt <= options.MaxAttempts; attempt++ {
		result.Attempts = attempt
		attemptCtx, cancel := context.WithTimeout(ctx, options.CheckTimeout)
		outcome = runProbeOnce(attemptCtx, check)
		cancel()
		if outcome.Status != StatusFailed || !outcome.Retryable || ctx.Err() != nil {
			break
		}
		if attempt < options.MaxAttempts {
			backoff := options.BackoffBase << (attempt - 1)
			if backoff > options.BackoffMax {
				backoff = options.BackoffMax
			}
			if sleepErr := options.Sleep(ctx, backoff); sleepErr != nil {
				break
			}
		}
	}
	finished := options.Now().UTC()
	result.FinishedAt = Timestamp(finished)
	result.DurationMS = finished.Sub(started).Milliseconds()

	result.Status = outcome.Status
	if outcome.Err != nil {
		code := outcome.ErrCode
		if code == "" || isTimeout(outcome.Err) {
			code = "deadline_exceeded"
		}
		result.Error = &ErrorDetail{Code: code, Message: RedactString(outcome.Err.Error())}
	}
	result.Evidence = SanitizeEvidence(outcome.Evidence)

	if len(outcome.Resources) == 0 && outcome.Cleanup == nil {
		return result
	}
	resources := make([]ResourceRecord, 0, len(outcome.Resources))
	for _, resource := range outcome.Resources {
		resources = append(resources, ResourceRecord{
			Kind:      RedactString(resource.Kind),
			Reference: RedactString(resource.Reference),
		})
	}
	result.Cleanup.Resources = resources
	if outcome.Cleanup == nil {
		return result
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, options.CleanupTimout)
	defer cancel()
	cleanupEvidence, cleanupErr := safeCleanup(cleanupCtx, outcome.Cleanup)
	result.Cleanup.Attempts = 1
	if cleanupErr != nil {
		result.Cleanup.Status = CleanupFailed
		result.Cleanup.Error = RedactString(cleanupErr.Error())
		result.Cleanup.DiagnosticRef = fmt.Sprintf("verify://cleanup/%s/%s", options.RunID, meta.ID)
		for i := range result.Cleanup.Resources {
			result.Cleanup.Resources[i].Preserved = true
		}
		return result
	}
	result.Cleanup.Status = CleanupSucceeded
	if len(cleanupEvidence) > 0 {
		result.Evidence = append(result.Evidence, SanitizeEvidence(cleanupEvidence)...)
	}
	return result
}

func runProbeOnce(ctx context.Context, check Check) (outcome Outcome) {
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome = Outcome{
				Status:  StatusFailed,
				ErrCode: "check_panicked",
				Err:     fmt.Errorf("check panicked: %v", recovered),
			}
		}
	}()
	return check.Probe(ctx)
}

func safeCleanup(ctx context.Context, cleanup CleanupFunc) (evidence []Evidence, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			evidence = nil
			err = fmt.Errorf("cleanup panicked: %v", recovered)
		}
	}()
	return cleanup(ctx)
}

// redactReport applies the recursive sanitizer to the fully marshaled report
// tree so no secret can survive in any field, regardless of which check
// attached it. The report is rebuilt from the redacted JSON tree.
func redactReport(report *Report) *Report {
	raw, err := json.Marshal(report)
	if err != nil {
		return report
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return report
	}
	redacted := RedactValue(tree)
	safe, err := json.Marshal(redacted)
	if err != nil {
		return report
	}
	var out Report
	if err := json.Unmarshal(safe, &out); err != nil {
		return report
	}
	return &out
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded)
}
