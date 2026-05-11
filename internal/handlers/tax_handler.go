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

// FetchGSTIN verifies GSTIN details
// @Summary Verify GSTIN details
// @Description Verifies GSTIN details through the configured GST lookup provider and falls back to local format validation when the provider is unavailable.
// @Tags Tax
// @Produce json
// @Security BearerAuth
// @Param gstin path string true "GSTIN"
// @Success 200 {object} services.GSTINLookupResult
// @Failure 500 {object} map[string]string
// @Router /utils/gstin/{gstin}/fetch [post]
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

// ImportGSTR2B imports GSTR-2B data
// @Summary Import GSTR-2B
// @Description Imports GSTR-2B data from government portal
// @Tags Tax
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param input body services.ImportGSTR2BInput true "Import parameters"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /tax/gstr-2b/import [post]
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

// GetReport retrieves a tax report
// @Summary Get tax report
// @Description Returns a tax report of the specified type
// @Tags Tax
// @Produce json
// @Security BearerAuth
// @Param type path string true "Report type"
// @Param period_start query string true "Period start date (2006-01-02)"
// @Param period_end query string true "Period end date (2006-01-02)"
// @Param filing_frequency query string false "Filing frequency"
// @Param export_format query string false "Export format"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /tax/reports/{type} [get]
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

// ExportReport exports a tax report
// @Summary Export tax report
// @Description Exports a tax report in the specified format
// @Tags Tax
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param type path string true "Report type"
// @Param input body services.GSTReportOptions true "Export options"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /tax/reports/{type}/export [post]
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

// GetReportRun retrieves a tax report run status
// @Summary Get tax report run
// @Description Returns the status of a tax report export run
// @Tags Tax
// @Produce json
// @Security BearerAuth
// @Param id path string true "Report Run ID"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /tax/report-runs/{id} [get]
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

// ListIntegrationAccounts lists tax integration accounts
// @Summary List tax integration accounts
// @Description Returns all tax integration accounts for the business
// @Tags Tax
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /tax/integrations [get]
func (h *TaxHandler) ListIntegrationAccounts(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	accounts, err := h.svc.ListIntegrationAccounts(c.Request.Context(), businessID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": accounts})
}

// UpsertIntegrationAccount creates or updates a tax integration account
// @Summary Upsert tax integration account
// @Description Creates or updates a tax integration account
// @Tags Tax
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string false "Integration Account ID"
// @Param input body services.UpsertGSTIntegrationAccountInput true "Account details"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /tax/integrations/{id} [post]
func (h *TaxHandler) UpsertIntegrationAccount(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.UpsertGSTIntegrationAccountInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	account, err := h.svc.UpsertIntegrationAccount(c.Request.Context(), businessID, c.Param("id"), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, account)
}

// ValidateIntegrationAccount validates a tax integration account
// @Summary Validate tax integration account
// @Description Validates credentials for a tax integration account
// @Tags Tax
// @Produce json
// @Security BearerAuth
// @Param id path string true "Integration Account ID"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /tax/integrations/{id}/validate [post]
func (h *TaxHandler) ValidateIntegrationAccount(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	account, err := h.svc.ValidateIntegrationAccount(c.Request.Context(), businessID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "account": account})
		return
	}
	c.JSON(http.StatusOK, account)
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
