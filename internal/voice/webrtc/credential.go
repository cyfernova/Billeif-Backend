package webrtc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

const maxPeerAuthorizationBytes = 8 << 10

var ErrPeerAuthorizationUnavailable = errors.New("voice peer authorization is unavailable")

// AuthorizationSource is the only secret-bearing capability exposed to a
// per-peer composition factory. Callers must use the value transiently and
// must never log, serialize, persist, or place it in model context.
type AuthorizationSource interface {
	Authorization(context.Context) (string, error)
}

// peerAuthorizationVault retains one validated Authorization value for the
// lifetime of a peer. Signaling owns rotation and scrubbing; downstream tool
// bindings receive only the narrow AuthorizationSource capability.
type peerAuthorizationVault struct {
	mu sync.RWMutex

	authorization []byte
	closed        bool
	closeOnce     sync.Once
}

func newPeerAuthorizationVault(authorization string) (*peerAuthorizationVault, error) {
	if !validPeerAuthorization(authorization) {
		return nil, ErrPeerAuthorizationUnavailable
	}
	return &peerAuthorizationVault{authorization: []byte(authorization)}, nil
}

func (vault *peerAuthorizationVault) Authorization(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", ErrPeerAuthorizationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if vault == nil {
		return "", ErrPeerAuthorizationUnavailable
	}
	vault.mu.RLock()
	defer vault.mu.RUnlock()
	if vault.closed || len(vault.authorization) == 0 {
		return "", ErrPeerAuthorizationUnavailable
	}
	return string(vault.authorization), nil
}

func (vault *peerAuthorizationVault) rotate(authorization string) error {
	if !validPeerAuthorization(authorization) || vault == nil {
		return ErrPeerAuthorizationUnavailable
	}
	replacement := []byte(authorization)
	vault.mu.Lock()
	defer vault.mu.Unlock()
	if vault.closed {
		scrubAuthorization(replacement)
		return ErrPeerAuthorizationUnavailable
	}
	previous := vault.authorization
	vault.authorization = replacement
	scrubAuthorization(previous)
	return nil
}

func (vault *peerAuthorizationVault) Close() error {
	if vault == nil {
		return nil
	}
	vault.closeOnce.Do(func() {
		vault.mu.Lock()
		vault.closed = true
		scrubAuthorization(vault.authorization)
		vault.authorization = nil
		vault.mu.Unlock()
	})
	return nil
}

func (*peerAuthorizationVault) String() string   { return "[redacted voice authorization]" }
func (*peerAuthorizationVault) GoString() string { return "[redacted voice authorization]" }

func (*peerAuthorizationVault) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct{}{})
}

func validPeerAuthorization(value string) bool {
	if len(value) < len("Bearer ")+1 || len(value) > maxPeerAuthorizationBytes {
		return false
	}
	separator := strings.IndexByte(value, ' ')
	if separator <= 0 || !strings.EqualFold(value[:separator], "Bearer") {
		return false
	}
	token := value[separator+1:]
	if token == "" {
		return false
	}
	for _, character := range token {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func scrubAuthorization(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
