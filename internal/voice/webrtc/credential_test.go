package webrtc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"invoice-backend/internal/voice/runtime"

	"github.com/stretchr/testify/require"
)

func TestPeerAuthorizationVaultRotatesAndScrubs(t *testing.T) {
	t.Parallel()

	vault, err := newPeerAuthorizationVault("Bearer first-sensitive-token")
	require.NoError(t, err)
	firstBacking := vault.authorization

	value, err := vault.Authorization(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Bearer first-sensitive-token", value)
	encoded, err := json.Marshal(vault)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "first-sensitive-token")
	require.NotContains(t, fmt.Sprint(vault), "first-sensitive-token")
	require.NotContains(t, fmt.Sprintf("%#v", vault), "first-sensitive-token")

	require.NoError(t, vault.rotate("Bearer second-sensitive-token"))
	for index, value := range firstBacking {
		if value != 0 {
			t.Fatalf("old authorization byte %d was not scrubbed", index)
		}
	}
	secondBacking := vault.authorization
	value, err = vault.Authorization(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Bearer second-sensitive-token", value)

	require.NoError(t, vault.Close())
	require.NoError(t, vault.Close())
	for index, value := range secondBacking {
		if value != 0 {
			t.Fatalf("closed authorization byte %d was not scrubbed", index)
		}
	}
	_, err = vault.Authorization(context.Background())
	require.ErrorIs(t, err, ErrPeerAuthorizationUnavailable)
	require.ErrorIs(t, vault.rotate("Bearer third-sensitive-token"), ErrPeerAuthorizationUnavailable)
}

func TestPeerAuthorizationVaultRejectsInvalidValuesAndContext(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"", "Basic abc", "Bearer", "Bearer ", "Bearer token with spaces",
		"Bearer token\nsmuggled", "Bearer " + string(make([]byte, maxPeerAuthorizationBytes)),
	} {
		if _, err := newPeerAuthorizationVault(value); !errors.Is(err, ErrPeerAuthorizationUnavailable) {
			t.Fatalf("newPeerAuthorizationVault(%q) error = %v", value, err)
		}
	}
	vault, err := newPeerAuthorizationVault("Bearer valid-token")
	require.NoError(t, err)
	t.Cleanup(func() { _ = vault.Close() })
	_, err = vault.Authorization(nil) //nolint:staticcheck // nil context is the contract under test.
	require.ErrorIs(t, err, ErrPeerAuthorizationUnavailable)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = vault.Authorization(canceled)
	require.ErrorIs(t, err, context.Canceled)
}

func TestSignalingPassesRotatingAuthorizationSourceOnlyAfterValidation(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	bindings := &fakeSTTBindingFactory{}
	peer := newFakeSignalingPeer()
	service := newTestSignalingService(t, testChannelARNs(requiredKVSChannelCount), SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:    &fakeSessionLookup{value: logicalSession},
		ICE:         &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities:  newFakeActivitySource(),
		Peers:       &fakePeerFactory{peer: peer},
		STTBindings: bindings,
	})
	t.Cleanup(func() { _ = service.Close() })

	first := signalingInvocation(logicalSession, attachBody(1))
	first.Authorization = "Bearer first-validated-token"
	response, err := service.Invoke(context.Background(), first)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	configs := bindings.configsSnapshot()
	require.Len(t, configs, 1)
	require.NotNil(t, configs[0].Authorization)
	value, err := configs[0].Authorization.Authorization(context.Background())
	require.NoError(t, err)
	require.Equal(t, first.Authorization, value)

	second := signalingInvocation(logicalSession, attachBody(2))
	second.Authorization = "Bearer refreshed-validated-token"
	response, err = service.Invoke(context.Background(), second)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Len(t, bindings.configsSnapshot(), 1, "refresh must rotate the existing source, not recreate the binding")
	value, err = configs[0].Authorization.Authorization(context.Background())
	require.NoError(t, err)
	require.Equal(t, second.Authorization, value)

	require.NoError(t, service.Close())
	_, err = configs[0].Authorization.Authorization(context.Background())
	require.ErrorIs(t, err, ErrPeerAuthorizationUnavailable)
}

func TestSignalingClosesAuthorizationSourceOnBindingFailure(t *testing.T) {
	t.Parallel()

	logicalSession := validSignalingSession()
	bindings := &fakeSTTBindingFactory{err: errors.New("constructor failed")}
	service := newTestSignalingService(t, testChannelARNs(requiredKVSChannelCount), SignalingDependencies{
		Authorization: &fakeAuthorizationResolver{identity: TrustedIdentity{
			UserID: logicalSession.UserID, BusinessID: logicalSession.BusinessID, ClientID: "client",
		}},
		Sessions:    &fakeSessionLookup{value: logicalSession},
		ICE:         &fakeICECredentialSource{credentials: validSignalingCredentials()},
		Activities:  newFakeActivitySource(),
		Peers:       &fakePeerFactory{peer: newFakeSignalingPeer()},
		STTBindings: bindings,
	})
	t.Cleanup(func() { _ = service.Close() })

	invocation := signalingInvocation(logicalSession, attachBody(1))
	invocation.Authorization = "Bearer failure-path-token"
	response, err := service.Invoke(context.Background(), invocation)
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	configs := bindings.configsSnapshot()
	require.Len(t, configs, 1)
	_, err = configs[0].Authorization.Authorization(context.Background())
	require.ErrorIs(t, err, ErrPeerAuthorizationUnavailable)
	require.NotContains(t, string(response.Body), "failure-path-token")
}

var _ runtime.InvocationHandler = (*SignalingService)(nil)
