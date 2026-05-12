package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

const (
	businessAuthBodyScopeKey     = "_business_auth_body_scope"
	businessAuthBodyTooLargeKey  = "_business_auth_body_too_large"
	validatedBusinessIDKey       = "validated_business_id"
	validatedBranchIDKey         = "validated_branch_id"
	validatedBranchScopeAllKey   = "validated_branch_scope_all"
	validatedBranchScopeIDsKey   = "validated_branch_scope_ids"
	maxBusinessAuthJSONBodyBytes = int64(1 << 20)
)

type businessAuthBodyScope struct {
	BusinessID string `json:"business_id"`
	BranchID   string `json:"branch_id"`
}

// BusinessAuth validates that the authenticated user has access to the requested business.
// It checks business and branch scope from query, header, path, and JSON body fields.
func BusinessAuth(authSvc *services.BusinessAuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		log := logger.FromContext(c.Request.Context()).Named("business_auth")
		userID := GetUserID(c)
		if userID == "" {
			log.Warn("business access denied: user not authenticated")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
			return
		}

		businessID := extractBusinessID(c)
		branchID := extractBranchID(c)
		if tooLarge, _ := c.Get(businessAuthBodyTooLargeKey); tooLarge == true {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body is too large"})
			return
		}
		if businessID == "" && branchID != "" {
			businessID = GetBusinessID(c)
		}
		if businessID == "" {
			businessID = GetBusinessID(c)
			if businessID == "" {
				c.Next()
				return
			}
		}

		if !authSvc.UserHasBusinessAccess(c.Request.Context(), userID, businessID) {
			log.Warn("business access denied", "user_id", userID, "business_id", businessID)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "access denied to this business"})
			return
		}

		allBranches, branchScopeIDs, branchScopeOK := authSvc.UserBranchScope(c.Request.Context(), userID, businessID)
		if !branchScopeOK {
			log.Warn("branch scope lookup denied", "user_id", userID, "business_id", businessID)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "access denied to this business"})
			return
		}

		if branchID != "" && !branchScopeAllows(allBranches, branchScopeIDs, branchID) {
			log.Warn("branch access denied", "user_id", userID, "business_id", businessID, "branch_id", branchID)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "access denied to this branch"})
			return
		}

		// Store validated business_id in context for handlers
		c.Set(validatedBusinessIDKey, businessID)
		if branchID != "" {
			c.Set(validatedBranchIDKey, branchID)
		}
		c.Set(validatedBranchScopeAllKey, allBranches)
		c.Set(validatedBranchScopeIDsKey, append([]string(nil), branchScopeIDs...))
		c.Next()
	}
}

// extractBusinessID extracts business_id from query, header, path, or JSON body.
func extractBusinessID(c *gin.Context) string {
	// Check query param first
	if id := c.Query("business_id"); id != "" {
		return id
	}

	if id := c.GetHeader("business_id"); id != "" {
		return id
	}

	if id := c.GetHeader("X-Business-ID"); id != "" {
		return id
	}

	// Check path param
	if id := c.Param("business_id"); id != "" {
		return id
	}

	if scope := extractBodyScope(c); scope.BusinessID != "" {
		return scope.BusinessID
	}

	return ""
}

func extractBranchID(c *gin.Context) string {
	if id := c.Query("branch_id"); id != "" {
		return id
	}
	if id := c.Param("branch_id"); id != "" {
		return id
	}
	if scope := extractBodyScope(c); scope.BranchID != "" {
		return scope.BranchID
	}
	return ""
}

func extractBodyScope(c *gin.Context) businessAuthBodyScope {
	if value, exists := c.Get(businessAuthBodyScopeKey); exists {
		if scope, ok := value.(businessAuthBodyScope); ok {
			return scope
		}
	}

	scope := businessAuthBodyScope{}
	defer func() {
		c.Set(businessAuthBodyScopeKey, scope)
	}()

	if c.Request == nil || c.Request.Body == nil {
		return scope
	}
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if !strings.Contains(contentType, "application/json") {
		return scope
	}
	if c.Request.ContentLength > maxBusinessAuthJSONBodyBytes {
		c.Set(businessAuthBodyTooLargeKey, true)
		return scope
	}

	limitedBody := http.MaxBytesReader(c.Writer, c.Request.Body, maxBusinessAuthJSONBodyBytes)
	body, err := io.ReadAll(limitedBody)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		c.Set(businessAuthBodyTooLargeKey, true)
		return scope
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return scope
	}

	_ = json.Unmarshal(body, &scope)
	return scope
}

func branchScopeAllows(allBranches bool, branchIDs []string, branchID string) bool {
	if branchID == "" || allBranches {
		return true
	}
	for _, allowedBranchID := range branchIDs {
		if allowedBranchID == branchID {
			return true
		}
	}
	return false
}
