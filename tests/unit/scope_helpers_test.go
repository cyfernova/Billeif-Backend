package unit

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func requireBusinessScope(c *gin.Context) (string, bool) {
	businessID, exists := c.Get("business_id")
	if !exists || businessID == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "business scope required"})
		return "", false
	}
	return businessID.(string), true
}

func requireUserScope(c *gin.Context) (string, bool) {
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user scope required"})
		return "", false
	}
	return userID.(string), true
}
