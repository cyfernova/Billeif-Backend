package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireAllBranchesRejectsCurrentRestrictedScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(validatedBranchScopeAllKey, false)
		c.Set(validatedBranchScopeIDsKey, []string{"11111111-1111-1111-1111-111111111111"})
		c.Next()
	})
	router.GET("/api/v1/invoices", RequireAllBranches(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/invoices", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("restricted current branch scope status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestRequireAllBranchesAllowsCurrentBusinessWideScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(validatedBranchScopeAllKey, true)
		c.Set(validatedBranchScopeIDsKey, []string(nil))
		c.Next()
	})
	router.GET("/api/v1/invoices", RequireAllBranches(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/invoices", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("business-wide current branch scope status = %d, want %d", response.Code, http.StatusNoContent)
	}
}
