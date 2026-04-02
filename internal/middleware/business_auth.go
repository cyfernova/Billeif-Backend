package middleware

import (
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

// BusinessAuth validates that the authenticated user has access to the requested business.
// It checks the business_id from query params or path params.
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
		if businessID == "" {
			// No business_id to validate, proceed
			c.Next()
			return
		}

		if !authSvc.UserHasBusinessAccess(c.Request.Context(), userID, businessID) {
			log.Warn("business access denied", "user_id", userID, "business_id", businessID)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "access denied to this business"})
			return
		}

		branchID := extractBranchID(c)
		if branchID != "" && !authSvc.UserHasBranchAccess(c.Request.Context(), userID, businessID, branchID) {
			log.Warn("branch access denied", "user_id", userID, "business_id", businessID, "branch_id", branchID)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "access denied to this branch"})
			return
		}

		// Store validated business_id in context for handlers
		c.Set("validated_business_id", businessID)
		c.Next()
	}
}

// extractBusinessID extracts business_id from query or path params.
// Note: We do not read the request body here to avoid EOF issues with API Gateway/proxies.
func extractBusinessID(c *gin.Context) string {
	// Check query param first
	if id := c.Query("business_id"); id != "" {
		return id
	}

	// Check path param
	if id := c.Param("business_id"); id != "" {
		return id
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
	return ""
}
