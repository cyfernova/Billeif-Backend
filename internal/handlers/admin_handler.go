package handlers

import (
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type AdminHandler struct {
	email *services.EmailService
	log   *logger.Logger
}

func NewAdminHandler(email *services.EmailService, log *logger.Logger) *AdminHandler {
	return &AdminHandler{email: email, log: log}
}

func (h *AdminHandler) ListEmails(c *gin.Context) {
	emails, err := h.email.ListAllCapturedEmails(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"emails": emails})
}
