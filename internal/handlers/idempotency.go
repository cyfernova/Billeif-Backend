package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func requireIdempotencyKey(c *gin.Context) (string, bool) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key header is required"})
		return "", false
	}
	return key, true
}
