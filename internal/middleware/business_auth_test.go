package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestExtractBusinessAndBranchIDFromJSONBodyRestoresBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := `{"business_id":"biz-body","branch_id":"branch-body","name":"Invoice"}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "/documents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	c.Request = req

	if got := extractBusinessID(c); got != "biz-body" {
		t.Fatalf("expected body business_id, got %q", got)
	}
	if got := extractBranchID(c); got != "branch-body" {
		t.Fatalf("expected body branch_id, got %q", got)
	}

	restored, err := io.ReadAll(c.Request.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(restored) != body {
		t.Fatalf("request body was not restored, got %q", string(restored))
	}
}

func TestExtractBodyScopeMarksOversizedJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "/documents", strings.NewReader(strings.Repeat("a", int(maxBusinessAuthJSONBodyBytes)+1)))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	_ = extractBodyScope(c)
	if tooLarge, _ := c.Get(businessAuthBodyTooLargeKey); tooLarge != true {
		t.Fatal("expected oversized JSON body to be marked")
	}
}

func TestExtractBusinessIDPrefersExplicitRequestScopeOverBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "/products?business_id=biz-query", strings.NewReader(`{"business_id":"biz-body"}`))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	if got := extractBusinessID(c); got != "biz-query" {
		t.Fatalf("expected query business_id to win, got %q", got)
	}
}

func TestExtractBranchIDUsesNamedRouteParam(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "branch_id", Value: "branch-route"}}
	c.Request = httptest.NewRequest(http.MethodPut, "/branches/branch-route", nil)

	if got := extractBranchID(c); got != "branch-route" {
		t.Fatalf("expected branch_id route param, got %q", got)
	}
}
