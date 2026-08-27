package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/services"

	"github.com/gin-gonic/gin"
)

// validateImageContentType checks that the Content-Type header is an allowed image MIME type.
// Returns the validated content type and true, or aborts the request and returns false.
func validateImageContentType(c *gin.Context) (string, bool) {
	ct := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	if ct == "" {
		ct = "image/png"
	}
	ct, err := services.NormalizeImageUploadContentType(ct)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return "", false
	}
	return ct, true
}

func requireUploadSizeBytes(c *gin.Context, maxBytes int64) (int64, bool) {
	rawSize := strings.TrimSpace(c.Query("size_bytes"))
	sizeBytes, err := strconv.ParseInt(rawSize, 10, 64)
	if err != nil || sizeBytes <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "size_bytes must be a positive integer"})
		return 0, false
	}
	if sizeBytes > maxBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "upload exceeds the maximum allowed size"})
		return 0, false
	}
	return sizeBytes, true
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
