package handlers

import (
	"net/http"
	"strconv"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type EmailConfigHandler struct {
	svc *services.EmailService
	log *logger.Logger
}

func NewEmailConfigHandler(svc *services.EmailService, log *logger.Logger) *EmailConfigHandler {
	return &EmailConfigHandler{svc: svc, log: log}
}

func (h *EmailConfigHandler) ListAccounts(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	accounts, err := h.svc.ListAccounts(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": accounts})
}

func (h *EmailConfigHandler) CreateAccount(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpsertEmailAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	account, err := h.svc.UpsertAccount(c.Request.Context(), businessID, "", input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, account)
}

func (h *EmailConfigHandler) UpdateAccount(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpsertEmailAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	account, err := h.svc.UpsertAccount(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		statusCode := http.StatusBadRequest
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, account)
}

func (h *EmailConfigHandler) ListDeliveries(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	deliveries, err := h.svc.ListDeliveries(c.Request.Context(), businessID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": deliveries})
}

func (h *EmailConfigHandler) SendTest(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.SendTestEmailInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	delivery, err := h.svc.SendTestEmail(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		statusCode := http.StatusBadRequest
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error(), "delivery": delivery})
		return
	}
	c.JSON(http.StatusOK, delivery)
}
