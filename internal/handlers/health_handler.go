package handlers

import (
	"net/http"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type HealthHandler struct {
	log *logger.Logger
}

func NewHealthHandler(log *logger.Logger) *HealthHandler {
	return &HealthHandler{log: log}
}

func (h *HealthHandler) Check(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"services": gin.H{
			"database":   "up",
			"localstack": "up",
		},
	})
}
