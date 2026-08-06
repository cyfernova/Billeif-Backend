package runtime

import (
	"context"
	"io"
	"sync"
)

// PersistentActivity owns a runtime-rooted context and activity lease for a
// peer or other session-scoped worker whose lifetime outlasts one invocation.
// Close is idempotent, cancels the worker context, and releases /ping busy
// state.
type PersistentActivity struct {
	once   sync.Once
	cancel context.CancelFunc
	lease  *ActivityLease
}

// AcquirePersistentActivity creates an activity context rooted in the runtime,
// not in the current HTTP invocation. Runtime shutdown cancels the context and
// waits for the returned closer to release its activity lease.
func (s *Server) AcquirePersistentActivity() (context.Context, io.Closer, error) {
	if s == nil || s.activities == nil || s.rootContext == nil {
		return nil, nil, ErrShuttingDown
	}
	lease, err := s.activities.acquire()
	if err != nil {
		return nil, nil, err
	}
	activityContext, cancel := context.WithCancel(s.rootContext)
	activity := &PersistentActivity{cancel: cancel, lease: lease}
	if err := activityContext.Err(); err != nil {
		_ = activity.Close()
		return nil, nil, ErrShuttingDown
	}
	return activityContext, activity, nil
}

// Close releases the persistent activity exactly once.
func (activity *PersistentActivity) Close() error {
	if activity == nil {
		return nil
	}
	var closeError error
	activity.once.Do(func() {
		if activity.cancel != nil {
			activity.cancel()
		}
		if activity.lease != nil {
			closeError = activity.lease.Close()
		}
	})
	return closeError
}
