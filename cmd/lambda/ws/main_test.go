package main

import (
	"context"
	"testing"

	"invoice-backend/internal/middleware"
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

func TestFirstNonEmptyTrimsValues(t *testing.T) {
	if got := firstNonEmpty("", "  business-1  "); got != "business-1" {
		t.Fatalf("unexpected value: %q", got)
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
