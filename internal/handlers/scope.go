package handlers

import (
	"net/http"
	"strings"

	"invoice-backend/internal/middleware"

	"github.com/gin-gonic/gin"
)

var allowedImageTypes = map[string]bool{
	"image/png":     true,
	"image/jpeg":    true,
	"image/gif":     true,
	"image/webp":    true,
	"image/svg+xml": true,
}

// validateImageContentType checks that the Content-Type header is an allowed image MIME type.
// Returns the validated content type and true, or aborts the request and returns false.
func validateImageContentType(c *gin.Context) (string, bool) {
	ct := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	if ct == "" {
		ct = "image/png"
	}
	if !allowedImageTypes[ct] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported content type; allowed: image/png, image/jpeg, image/gif, image/webp, image/svg+xml"})
		return "", false
	}
	return ct, true
}

func requireBusinessScope(c *gin.Context) (string, bool) {
	return requireEffectiveBusinessScope(c, "")
}

// requireEffectiveBusinessScope enforces request-scoped business ownership.
// If requestedBusinessID is provided, it must match the authenticated business scope.
func requireEffectiveBusinessScope(c *gin.Context, requestedBusinessID string) (string, bool) {
	businessID := middleware.GetEffectiveBusinessID(c)
	if businessID == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return "", false
	}

	requestedBusinessID = strings.TrimSpace(requestedBusinessID)
	if requestedBusinessID != "" && requestedBusinessID != businessID {
		c.JSON(http.StatusForbidden, gin.H{"error": "business_id does not match authenticated scope"})
		return "", false
	}

	return businessID, true
}

func requireUserScope(c *gin.Context) (string, bool) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return "", false
	}
	return userID, true
}
