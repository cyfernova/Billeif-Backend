package session

import (
	"context"
	"reflect"
	"sync"
	"time"
)

const (
	DefaultLeaseHeartbeatWriteInterval = 30 * time.Second
	maxLeaseHeartbeatInterval          = DefaultLeaseHeartbeatWriteInterval
	maxLeaseHeartbeatTimeout           = 5 * time.Second
)

type LeaseHeartbeatConfig struct {
	Context context.Context
	Renewer LeaseRenewer
	Session Session

	LeaseDuration    time.Duration
	MinWriteInterval time.Duration
	WriteTimeout     time.Duration
	Now              func() time.Time
}

type leaseHeartbeatJob struct {
	request       LeaseRenewal
	expectedLease time.Time
}

// LeaseHeartbeat coalesces untrusted client heartbeat events into serialized,
// server-timed lease writes. A durable failure is terminal: Done closes with
// Err set, allowing the peer owner to tear down the connection rather than
// silently claiming that its lease remains healthy.
type LeaseHeartbeat struct {
	mu sync.Mutex

	ctx     context.Context
	cancel  context.CancelFunc
	renewer LeaseRenewer
	now     func() time.Time

	scope            Scope
	sessionID        string
	branchID         string
	runtimeSessionID string
	expectedLease    time.Time
	expiresAt        time.Time
	leaseDuration    time.Duration
	minInterval      time.Duration
	writeTimeout     time.Duration
	lastWriteAt      time.Time
	inFlight         bool
	closed           bool
	err              error

	jobs chan leaseHeartbeatJob
	done chan struct{}
}

func NewLeaseHeartbeat(config LeaseHeartbeatConfig) (*LeaseHeartbeat, error) {
	if config.Context == nil || config.Context.Err() != nil || nilLeaseRenewer(config.Renewer) ||
		config.LeaseDuration < 30*time.Second || config.LeaseDuration > 5*time.Minute ||
		config.MinWriteInterval <= 0 || config.MinWriteInterval > maxLeaseHeartbeatInterval ||
		config.MinWriteInterval >= config.LeaseDuration ||
		config.WriteTimeout <= 0 || config.WriteTimeout > maxLeaseHeartbeatTimeout {
		return nil, ErrInvalidRequest
	}
	nowSource := config.Now
	if nowSource == nil {
		nowSource = time.Now
	} else if reflect.ValueOf(nowSource).IsNil() {
		return nil, ErrInvalidRequest
	}
	now, ok := safeLeaseHeartbeatNow(nowSource)
	if !ok || !validLeaseHeartbeatSession(config.Session, now) {
		return nil, ErrInvalidRequest
	}
	ctx, cancel := context.WithCancel(config.Context)
	lastWriteAt := config.Session.UpdatedAt.UTC()
	if lastWriteAt.IsZero() || lastWriteAt.After(now) {
		lastWriteAt = now
	}
	heartbeat := &LeaseHeartbeat{
		ctx: ctx, cancel: cancel, renewer: config.Renewer, now: nowSource,
		scope: sessionScope(&config.Session), sessionID: config.Session.ID, branchID: config.Session.BranchID,
		runtimeSessionID: config.Session.RuntimeSessionID, expectedLease: config.Session.LeaseExpiresAt.UTC(),
		expiresAt: config.Session.ExpiresAt.UTC(), leaseDuration: config.LeaseDuration,
		minInterval: config.MinWriteInterval, writeTimeout: config.WriteTimeout, lastWriteAt: lastWriteAt,
		jobs: make(chan leaseHeartbeatJob, 1), done: make(chan struct{}),
	}
	go heartbeat.run()
	return heartbeat, nil
}

// Heartbeat is non-blocking with respect to DynamoDB. It uses only the server
// clock and coalesces bursts while a write is queued or in flight.
func (heartbeat *LeaseHeartbeat) Heartbeat() error {
	if heartbeat == nil {
		return ErrLeaseHeartbeatFailed
	}
	now, ok := safeLeaseHeartbeatNow(heartbeat.now)
	heartbeat.mu.Lock()
	defer heartbeat.mu.Unlock()
	if heartbeat.err != nil || heartbeat.closed {
		return ErrLeaseHeartbeatFailed
	}
	if !ok || !heartbeat.expectedLease.After(now) || !heartbeat.expiresAt.After(now) {
		heartbeat.failLocked()
		return ErrLeaseHeartbeatFailed
	}
	if heartbeat.inFlight || now.Before(heartbeat.lastWriteAt.Add(heartbeat.minInterval)) {
		return nil
	}
	newLease := now.Add(heartbeat.leaseDuration)
	if newLease.After(heartbeat.expiresAt) {
		newLease = heartbeat.expiresAt
	}
	// Once the lease is already capped at the product expiry, another write
	// cannot make it safer. The reconciler will continue to observe it as live.
	if !newLease.After(heartbeat.expectedLease) {
		heartbeat.lastWriteAt = now
		return nil
	}
	request := LeaseRenewal{
		Scope: heartbeat.scope, SessionID: heartbeat.sessionID, ExpectedBranchID: heartbeat.branchID,
		RuntimeSessionID: heartbeat.runtimeSessionID, ExpectedLeaseExpiresAt: heartbeat.expectedLease,
		ExpectedExpiresAt: heartbeat.expiresAt, RenewedAt: now, LeaseDuration: heartbeat.leaseDuration,
	}
	heartbeat.inFlight = true
	heartbeat.lastWriteAt = now
	select {
	case heartbeat.jobs <- leaseHeartbeatJob{request: request, expectedLease: newLease}:
		return nil
	case <-heartbeat.ctx.Done():
		heartbeat.failLocked()
		return ErrLeaseHeartbeatFailed
	default:
		// inFlight guarantees at most one queued job. Reaching this branch means
		// internal state was violated, so fail closed instead of dropping a write.
		heartbeat.failLocked()
		return ErrLeaseHeartbeatFailed
	}
}

func (heartbeat *LeaseHeartbeat) Done() <-chan struct{} {
	if heartbeat == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return heartbeat.done
}

func (heartbeat *LeaseHeartbeat) Err() error {
	if heartbeat == nil {
		return ErrLeaseHeartbeatFailed
	}
	heartbeat.mu.Lock()
	defer heartbeat.mu.Unlock()
	return heartbeat.err
}

func (heartbeat *LeaseHeartbeat) Close() error {
	if heartbeat == nil {
		return nil
	}
	heartbeat.mu.Lock()
	if !heartbeat.closed {
		heartbeat.closed = true
		heartbeat.cancel()
	}
	heartbeat.mu.Unlock()
	return nil
}

func (heartbeat *LeaseHeartbeat) run() {
	defer close(heartbeat.done)
	for {
		select {
		case <-heartbeat.ctx.Done():
			return
		case job := <-heartbeat.jobs:
			writeContext, cancel := context.WithTimeout(heartbeat.ctx, heartbeat.writeTimeout)
			renewed, err := safeRenewLease(writeContext, heartbeat.renewer, job.request)
			cancel()
			if err != nil || !validLeaseHeartbeatResult(renewed, job.request, job.expectedLease) {
				heartbeat.mu.Lock()
				if heartbeat.ctx.Err() == nil {
					heartbeat.failLocked()
				}
				heartbeat.mu.Unlock()
				return
			}
			heartbeat.mu.Lock()
			if heartbeat.closed {
				heartbeat.mu.Unlock()
				return
			}
			heartbeat.expectedLease = renewed.LeaseExpiresAt.UTC()
			heartbeat.inFlight = false
			heartbeat.mu.Unlock()
		}
	}
}

func (heartbeat *LeaseHeartbeat) failLocked() {
	if heartbeat.err == nil {
		heartbeat.err = ErrLeaseHeartbeatFailed
	}
	heartbeat.inFlight = false
	heartbeat.cancel()
}

func safeRenewLease(ctx context.Context, renewer LeaseRenewer, request LeaseRenewal) (result *Session, err error) {
	defer func() {
		if recover() != nil {
			result = nil
			err = ErrLeaseHeartbeatFailed
		}
	}()
	result, err = renewer.RenewLease(ctx, request)
	if err != nil {
		return nil, ErrLeaseHeartbeatFailed
	}
	return result, nil
}

func safeLeaseHeartbeatNow(now func() time.Time) (value time.Time, ok bool) {
	defer func() {
		if recover() != nil {
			value = time.Time{}
			ok = false
		}
	}()
	value = now().UTC()
	return value, !value.IsZero()
}

func validLeaseHeartbeatSession(value Session, now time.Time) bool {
	return validSessionID(value.ID) && validRuntimeLeaseID(value.RuntimeSessionID) &&
		validateScope(Scope{UserID: value.UserID, BusinessID: value.BusinessID}) == nil &&
		(value.BranchID == "" || scopeAllowsBranch(sessionScope(&value), value.BranchID)) &&
		value.Status == StatusActive && value.RuntimeState == RuntimeStateRunning && !value.CapacityReleased &&
		!value.UpdatedAt.IsZero() && value.LeaseExpiresAt.After(now) && value.ExpiresAt.After(now) &&
		!value.LeaseExpiresAt.After(value.ExpiresAt)
}

func validLeaseHeartbeatResult(value *Session, request LeaseRenewal, expectedLease time.Time) bool {
	return value != nil && value.ID == request.SessionID && value.RuntimeSessionID == request.RuntimeSessionID &&
		value.UserID == request.Scope.UserID && value.BusinessID == request.Scope.BusinessID && value.BranchID == request.ExpectedBranchID &&
		value.Status == StatusActive && value.RuntimeState == RuntimeStateRunning && !value.CapacityReleased &&
		value.LeaseExpiresAt.Equal(expectedLease) && value.ExpiresAt.Equal(request.ExpectedExpiresAt) &&
		value.UpdatedAt.Equal(request.RenewedAt) && !value.LeaseExpiresAt.After(value.ExpiresAt)
}

func nilLeaseRenewer(value LeaseRenewer) bool {
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
