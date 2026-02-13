package handlers

import (
	"net/http"

	"invoice-backend/internal/middleware"

	"github.com/gin-gonic/gin"
)

func requireBusinessScope(c *gin.Context) (string, bool) {
	businessID := middleware.GetEffectiveBusinessID(c)
	if businessID == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
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
