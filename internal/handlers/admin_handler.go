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

// ListEmails retrieves all emails captured in local storage
// @Summary List captured emails
// @Description [Admin Only] Returns a list of all emails captured by the system (for development/testing).
// @Tags Admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /admin/local-emails [get]
func (h *AdminHandler) ListEmails(c *gin.Context) {
	log := logger.FromContext(c.Request.Context()).Named("admin_handler").With("operation", "list_emails")
	emails, err := h.email.ListAllCapturedEmails(c.Request.Context())
	if err != nil {
		log.Error("failed to list captured emails", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Info("captured emails listed", "count", len(emails))

	c.JSON(http.StatusOK, gin.H{"emails": emails})
}
