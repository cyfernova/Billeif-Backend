package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequirePlatformOperatorUsesOnlyConfiguredVerifiedGroupAndFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		configured string
		role       string
		groups     []string
		wantStatus int
		wantCode   string
	}{
		{name: "configured operator", configured: "platform_operator", role: "viewer", groups: []string{"platform_operator"}, wantStatus: http.StatusNoContent},
		{name: "admin is not operator", configured: "platform_operator", role: "admin", groups: []string{"admin"}, wantStatus: http.StatusForbidden, wantCode: "operator_access_denied"},
		{name: "owner is not operator", configured: "platform_operator", role: "owner", groups: []string{"owner"}, wantStatus: http.StatusForbidden, wantCode: "operator_access_denied"},
		{name: "missing configuration", role: "admin", groups: []string{"platform_operator"}, wantStatus: http.StatusForbidden, wantCode: "operator_not_configured"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/operator", func(c *gin.Context) {
				c.Set("user_id", "verified-subject")
				c.Set("role", test.role)
				c.Set("groups", test.groups)
				c.Next()
			}, RequirePlatformOperator(test.configured), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			request := httptest.NewRequest(http.MethodGet, "/operator", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantCode != "" {
				var body map[string]string
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body["code"] != test.wantCode {
					t.Fatalf("body = %#v, %v", body, err)
				}
			}
		})
	}
}
