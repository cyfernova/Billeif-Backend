package reconciler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"invoice-backend/internal/voice/session"
)

const (
	defaultBatchSize = int32(10)
	maxBatchSize     = int32(100)
)

var (
	ErrInvalidOptions     = errors.New("invalid voice reconciler options")
	ErrLeaseQuery         = errors.New("voice lease reconciliation query failed")
	ErrCandidateReconcile = errors.New("voice lease candidate reconciliation failed")
)

type LeaseStore interface {
	ExpiredLeases(context.Context, time.Time, int32) ([]*session.Session, error)
	ReleaseExpired(context.Context, *session.Session, time.Time) (*session.Session, session.ReleaseState, error)
	MarkClosed(context.Context, session.Scope, string, string, time.Time) error
}

// LeaseMetrics receives only a count of expired session leases whose capacity
// was recovered. It never receives tenant or session identifiers.
type LeaseMetrics interface {
	SessionLeaksRecovered(int)
}

type Options struct {
	Store   LeaseStore
	Stopper session.RuntimeStopper
	Metrics LeaseMetrics

	AgentRuntimeARN       string
	AgentRuntimeQualifier string
	BatchSize             int32
	Now                   func() time.Time
}

type Result struct {
	Examined int `json:"examined"`
	Released int `json:"released"`
	Stopped  int `json:"stopped"`
	Closed   int `json:"closed"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

type Handler struct {
	store   LeaseStore
	stopper session.RuntimeStopper
	metrics LeaseMetrics

	agentRuntimeARN       string
	agentRuntimeQualifier string
	batchSize             int32
	now                   func() time.Time
}

func New(options Options) (*Handler, error) {
	if nilInterface(options.Store) || nilInterface(options.Stopper) ||
		strings.TrimSpace(options.AgentRuntimeARN) == "" || strings.TrimSpace(options.AgentRuntimeQualifier) == "" {
		return nil, ErrInvalidOptions
	}
	batchSize := options.BatchSize
	if batchSize == 0 {
		batchSize = defaultBatchSize
	}
	if batchSize < 1 || batchSize > maxBatchSize {
		return nil, ErrInvalidOptions
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Handler{
		store: options.Store, stopper: options.Stopper, metrics: options.Metrics,
		agentRuntimeARN:       strings.TrimSpace(options.AgentRuntimeARN),
		agentRuntimeQualifier: strings.TrimSpace(options.AgentRuntimeQualifier),
		batchSize:             batchSize, now: now,
	}, nil
}

func (handler *Handler) Handle(ctx context.Context) (result Result, resultErr error) {
	if handler == nil || ctx == nil {
		return Result{}, ErrInvalidOptions
	}
	defer func() {
		if result.Released > 0 {
			safeSessionLeaksRecovered(handler.metrics, result.Released)
		}
	}()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	cutoff := handler.now().UTC()
	candidates, err := handler.store.ExpiredLeases(ctx, cutoff, handler.batchSize)
	if err != nil {
		return Result{}, ErrLeaseQuery
	}
	failures := make([]error, 0)
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(errors.Join(failures...), err)
		}
		result.Examined++
		if !validCandidate(candidate) {
			result.Failed++
			failures = append(failures, candidateFailure(candidate))
			continue
		}
		if _, duplicate := seen[candidate.ID]; duplicate {
			result.Skipped++
			continue
		}
		seen[candidate.ID] = struct{}{}

		current, state, releaseErr := handler.store.ReleaseExpired(ctx, candidate, cutoff)
		if releaseErr != nil || current == nil || current.ID != candidate.ID {
			result.Failed++
			failures = append(failures, candidateFailure(candidate))
			continue
		}
		if state.AlreadyClosed || state.LeaseRenewed {
			result.Skipped++
			continue
		}
		if !state.ShouldStop || strings.TrimSpace(current.RuntimeSessionID) == "" {
			result.Failed++
			failures = append(failures, candidateFailure(candidate))
			continue
		}
		if state.Released {
			result.Released++
		}
		if err := handler.stopper.StopRuntimeSession(ctx, session.RuntimeTarget{
			AgentRuntimeARN: handler.agentRuntimeARN, RuntimeSessionID: current.RuntimeSessionID,
			Qualifier: handler.agentRuntimeQualifier,
		}); err != nil {
			result.Failed++
			failures = append(failures, candidateFailure(candidate))
			continue
		}
		result.Stopped++
		if err := handler.store.MarkClosed(ctx, scopeFor(current), current.ID, current.BranchID, cutoff); err != nil {
			result.Failed++
			failures = append(failures, candidateFailure(candidate))
			continue
		}
		result.Closed++
	}
	return result, errors.Join(failures...)
}

func safeSessionLeaksRecovered(metrics LeaseMetrics, count int) {
	if count <= 0 || nilInterface(metrics) {
		return
	}
	defer func() { _ = recover() }()
	metrics.SessionLeaksRecovered(count)
}

func validCandidate(candidate *session.Session) bool {
	return candidate != nil && strings.HasPrefix(candidate.ID, "voice_") && len(candidate.ID) <= 96 &&
		strings.TrimSpace(candidate.UserID) != "" && strings.TrimSpace(candidate.BusinessID) != "" &&
		!candidate.LeaseExpiresAt.IsZero()
}

func candidateFailure(candidate *session.Session) error {
	if candidate == nil || candidate.ID == "" {
		return ErrCandidateReconcile
	}
	return fmt.Errorf("%w: %s", ErrCandidateReconcile, candidate.ID)
}

func scopeFor(value *session.Session) session.Scope {
	scope := session.Scope{UserID: value.UserID, BusinessID: value.BusinessID}
	if value.BranchID == "" {
		scope.AllBranches = true
	} else {
		scope.AllowedBranchIDs = []string{value.BranchID}
	}
	return scope
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
