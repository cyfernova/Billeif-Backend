package handlers

import (
	"errors"
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/internal/utils"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type JournalHandler struct {
	svc *services.JournalService
	log *logger.Logger
}

func NewJournalHandler(svc *services.JournalService, log *logger.Logger) *JournalHandler {
	return &JournalHandler{svc: svc, log: log}
}

// Create creates a new journal entry
// @Summary Create journal
// @Description Creates a new journal entry for the business
// @Tags Journals
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.CreateJournalInput true "Journal details"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /journals [post]
func (h *JournalHandler) Create(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateJournalInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	journal, err := h.svc.CreateAuthorized(c.Request.Context(), businessID, input, accountingPostingAuthorization(c, "journal:new"))
	if err != nil {
		if errors.Is(err, services.ErrAccountingPeriodLocked) {
			writeAccountingStepUpRequired(c)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, journal)
}

// Get retrieves a journal by ID
// @Summary Get journal
// @Description Returns a journal by ID
// @Tags Journals
// @Produce json
// @Security BearerAuth
// @Param id path string true "Journal ID"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /journals/{id} [get]
func (h *JournalHandler) Get(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	journal, err := h.svc.GetByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "journal not found"})
		return
	}
	c.JSON(http.StatusOK, journal)
}

// List returns all journals for a business
// @Summary List journals
// @Description Returns all journals for the business
// @Tags Journals
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /journals [get]
func (h *JournalHandler) List(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page, limit := utils.ParsePagination(c)
	journals, total, err := h.svc.List(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": journals, "total": total, "page": page, "limit": limit})
}

// Update updates an existing journal
// @Summary Update journal
// @Description Updates an existing journal by ID
// @Tags Journals
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Journal ID"
// @Param input body services.CreateJournalInput true "Journal update details"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /journals/{id} [put]
func (h *JournalHandler) Update(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.CreateJournalInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	journal, err := h.svc.UpdateAuthorized(c.Request.Context(), businessID, c.Param("id"), input, accountingPostingAuthorization(c, "journal:"+c.Param("id")))
	if err != nil {
		if errors.Is(err, services.ErrAccountingPeriodLocked) {
			writeAccountingStepUpRequired(c)
			return
		}
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "only draft journals can be updated" || err.Error() == "journal is not balanced" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, journal)
}

// Delete deletes a journal
// @Summary Delete journal
// @Description Deletes a journal by ID
// @Tags Journals
// @Produce json
// @Security BearerAuth
// @Param id path string true "Journal ID"
// @Success 204 {string} string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /journals/{id} [delete]
func (h *JournalHandler) Delete(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	if err := h.svc.DeleteByBusiness(c.Request.Context(), businessID, c.Param("id")); err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "only draft journals can be deleted" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// Post posts a journal
// @Summary Post journal
// @Description Posts a journal by ID
// @Tags Journals
// @Produce json
// @Security BearerAuth
// @Param id path string true "Journal ID"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /journals/{id}/post [post]
func (h *JournalHandler) Post(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	journal, err := h.svc.PostAuthorized(c.Request.Context(), businessID, c.Param("id"), accountingPostingAuthorization(c, "journal:"+c.Param("id")))
	if err != nil {
		if errors.Is(err, services.ErrAccountingPeriodLocked) {
			writeAccountingStepUpRequired(c)
			return
		}
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "journal already posted" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, journal)
}

// Reverse reverses a journal
// @Summary Reverse journal
// @Description Reverses a posted journal by ID
// @Tags Journals
// @Produce json
// @Security BearerAuth
// @Param id path string true "Journal ID"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /journals/{id}/reverse [post]
func (h *JournalHandler) Reverse(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	journal, err := h.svc.ReverseByBusiness(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "only posted journals can be reversed" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, journal)
}
