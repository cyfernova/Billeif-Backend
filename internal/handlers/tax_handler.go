package handlers

import (
	"net/http"
	"time"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type TaxHandler struct {
	svc *services.TaxComplianceService
	log *logger.Logger
}

func NewTaxHandler(svc *services.TaxComplianceService, log *logger.Logger) *TaxHandler {
	return &TaxHandler{svc: svc, log: log}
}

func (h *TaxHandler) FetchGSTIN(c *gin.Context) {
	if _, ok := requireBusinessScope(c); !ok {
		return
	}
	result, err := h.svc.FetchGSTIN(c.Request.Context(), c.Param("gstin"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TaxHandler) ImportGSTR2B(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.ImportGSTR2BInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	gstrImport, results, err := h.svc.ImportGSTR2B(c.Request.Context(), businessID, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"import": gstrImport, "results": results})
}

func (h *TaxHandler) GetReport(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	opts, err := parseReportOptions(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	report, err := h.svc.GetReport(c.Request.Context(), businessID, c.Param("type"), opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, report)
}

func (h *TaxHandler) ExportReport(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.GSTReportOptions
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if userID, exists := c.Get("user_id"); exists {
		if typed, ok := userID.(string); ok {
			input.CreatedBy = typed
		}
	}
	run, err := h.svc.ExportReport(c.Request.Context(), businessID, c.Param("type"), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, run)
}

func (h *TaxHandler) GetReportRun(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	run, err := h.svc.GetReportRun(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "report run not found"})
		return
	}
	c.JSON(http.StatusOK, run)
}

func parseReportOptions(c *gin.Context) (services.GSTReportOptions, error) {
	start, err := time.Parse("2006-01-02", c.Query("period_start"))
	if err != nil {
		return services.GSTReportOptions{}, err
	}
	end, err := time.Parse("2006-01-02", c.Query("period_end"))
	if err != nil {
		return services.GSTReportOptions{}, err
	}
	return services.GSTReportOptions{
		PeriodStart:     start,
		PeriodEnd:       end,
		FilingFrequency: c.Query("filing_frequency"),
		ExportFormat:    c.Query("export_format"),
	}, nil
}
