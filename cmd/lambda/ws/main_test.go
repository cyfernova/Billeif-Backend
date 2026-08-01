package main

import (
	"context"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/internal/middleware"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/events"
	"github.com/golang-jwt/jwt/v4"
)

type fakeBusinessAccessChecker struct {
	allowed    bool
	calls      int
	userID     string
	businessID string
}

func (f *fakeBusinessAccessChecker) UserHasBusinessAccess(_ context.Context, userID, businessID string) bool {
	f.calls++
	f.userID = userID
	f.businessID = businessID
	return f.allowed
}

func TestDecodeWebSocketBodyJSON(t *testing.T) {
	var payload struct {
		Action string `json:"action"`
	}
	err := decodeWebSocketBody(events.APIGatewayWebsocketProxyRequest{
		Body: `{"action":"voice.start"}`,
	}, &payload)
	if err != nil {
		t.Fatalf("expected JSON body to decode: %v", err)
	}
	if payload.Action != "voice.start" {
		t.Fatalf("unexpected action: %s", payload.Action)
	}
}

func TestDecodeWebSocketBodyRejectsInvalidBase64(t *testing.T) {
	var payload struct{}
	err := decodeWebSocketBody(events.APIGatewayWebsocketProxyRequest{
		Body:            "not-base64",
		IsBase64Encoded: true,
	}, &payload)
	if err == nil {
		t.Fatal("expected invalid base64 to fail")
	}
}

func TestVoicePilotDisabledResponseFailsClosed(t *testing.T) {
	response, disabled := voicePilotDisabledResponse(&config.LambdaVoiceConfig{Enabled: false})
	if !disabled {
		t.Fatal("expected disabled voice pilot to fail closed")
	}
	if response.StatusCode != 503 || response.Body != "voice pilot is disabled" {
		t.Fatalf("unexpected disabled response: status=%d body=%q", response.StatusCode, response.Body)
	}

	_, disabled = voicePilotDisabledResponse(&config.LambdaVoiceConfig{Enabled: true})
	if disabled {
		t.Fatal("expected enabled voice pilot to continue")
	}
}

func TestFirstNonEmptyTrimsValues(t *testing.T) {
	if got := firstNonEmpty("", "  business-1  "); got != "business-1" {
		t.Fatalf("unexpected value: %q", got)
	}
}

func TestStripBearerPrefix(t *testing.T) {
	if got := stripBearerPrefix("Bearer access-token"); got != "access-token" {
		t.Fatalf("unexpected stripped bearer token: %q", got)
	}
	if got := stripBearerPrefix("  bearer   spaced-token  "); got != "spaced-token" {
		t.Fatalf("unexpected stripped lowercase bearer token: %q", got)
	}
	if got := stripBearerPrefix("raw-token"); got != "raw-token" {
		t.Fatalf("unexpected raw token: %q", got)
	}
}

func TestExtractAuthTokenAcceptsAuthorizationQuery(t *testing.T) {
	token := "Bearer query-token"
	got := extractAuthToken(events.APIGatewayWebsocketProxyRequest{
		QueryStringParameters: map[string]string{
			"authorization": token,
		},
	})
	if got != token {
		t.Fatalf("unexpected token: %q", got)
	}
}

func TestAuthorizeWebSocketBusinessScopeUsesRequestedBusinessWithoutClaim(t *testing.T) {
	checker := setupBusinessAccessChecker(t, true)
	businessID, response, ok := authorizeWebSocketBusinessScope(
		context.Background(),
		events.APIGatewayWebsocketProxyRequest{
			QueryStringParameters: map[string]string{
				"business_id": "biz-query",
			},
		},
		&middleware.CognitoClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"}},
		"conn-1",
	)

	if !ok {
		t.Fatalf("expected business scope to authorize, got status %d body %q", response.StatusCode, response.Body)
	}
	if businessID != "biz-query" {
		t.Fatalf("unexpected business id: %q", businessID)
	}
	if checker.calls != 1 || checker.userID != "user-1" || checker.businessID != "biz-query" {
		t.Fatalf("unexpected access check: calls=%d user=%q business=%q", checker.calls, checker.userID, checker.businessID)
	}
}

func TestAuthorizeWebSocketBusinessScopeRejectsClaimMismatch(t *testing.T) {
	checker := setupBusinessAccessChecker(t, true)
	_, response, ok := authorizeWebSocketBusinessScope(
		context.Background(),
		events.APIGatewayWebsocketProxyRequest{
			QueryStringParameters: map[string]string{
				"business_id": "biz-query",
			},
		},
		&middleware.CognitoClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"}, BusinessID: "biz-claim"},
		"conn-1",
	)

	if ok {
		t.Fatal("expected business scope mismatch to fail")
	}
	if response.StatusCode != 403 {
		t.Fatalf("unexpected status: %d", response.StatusCode)
	}
	if checker.calls != 0 {
		t.Fatalf("expected access checker not to be called, got %d calls", checker.calls)
	}
}

func TestAuthorizeWebSocketBusinessScopeRejectsDeniedBusinessAccess(t *testing.T) {
	checker := setupBusinessAccessChecker(t, false)
	_, response, ok := authorizeWebSocketBusinessScope(
		context.Background(),
		events.APIGatewayWebsocketProxyRequest{
			Headers: map[string]string{
				"x-business-id": "biz-header",
			},
		},
		&middleware.CognitoClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"}},
		"conn-1",
	)

	if ok {
		t.Fatal("expected denied business access to fail")
	}
	if response.StatusCode != 403 {
		t.Fatalf("unexpected status: %d", response.StatusCode)
	}
	if checker.calls != 1 || checker.businessID != "biz-header" {
		t.Fatalf("unexpected access check: calls=%d business=%q", checker.calls, checker.businessID)
	}
}

func TestCanReuseVoiceSessionForStart(t *testing.T) {
	session := &services.VoiceLambdaSession{
		ConnectionID: "conn-1",
		BusinessID:   "biz-1",
		Status:       services.VoiceSessionStatusRunning,
	}
	if !canReuseVoiceSessionForStart(session, " conn-1 ", " biz-1 ") {
		t.Fatal("expected running session on same connection and business to be reusable")
	}

	session.Status = services.VoiceSessionStatusStarting
	if !canReuseVoiceSessionForStart(session, "conn-1", "biz-1") {
		t.Fatal("expected starting session on same connection and business to be reusable")
	}

	session.Status = services.VoiceSessionStatusStopping
	if canReuseVoiceSessionForStart(session, "conn-1", "biz-1") {
		t.Fatal("expected stopping session not to be reused for start")
	}

	session.Status = services.VoiceSessionStatusRunning
	session.BusinessID = "biz-2"
	if canReuseVoiceSessionForStart(session, "conn-1", "biz-1") {
		t.Fatal("expected business mismatch not to be reused")
	}
}

func setupBusinessAccessChecker(t *testing.T, allowed bool) *fakeBusinessAccessChecker {
	t.Helper()
	previousAuthSvc := wsAuthSvc
	previousLog := wsLog
	checker := &fakeBusinessAccessChecker{allowed: allowed}
	wsAuthSvc = checker
	wsLog = logger.New()
	t.Cleanup(func() {
		wsAuthSvc = previousAuthSvc
		wsLog = previousLog
	})
	return checker
}
