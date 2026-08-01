package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/reporting"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type ReportHandler struct {
	svc       *services.ReportService
	inventory *services.InventoryService
	log       *logger.Logger
}

func NewReportHandler(svc *services.ReportService, inventory *services.InventoryService, log *logger.Logger) *ReportHandler {
	return &ReportHandler{svc: svc, inventory: inventory, log: log}
}

// Catalog returns the available report types
// @Summary Report catalog
// @Description Returns all available report types
// @Tags Reports
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Router /reports/catalog [get]
func (h *ReportHandler) Catalog(c *gin.Context) {
	if _, ok := requireBusinessScope(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": h.svc.Catalog()})
}

// Query executes a report query
// @Summary Query report
// @Description Executes a report query and returns results
// @Tags Reports
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param key path string true "Report key"
// @Param input body services.ReportQueryInput true "Query parameters"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /reports/{key}/query [post]
func (h *ReportHandler) Query(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.ReportQueryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.applyReportScope(c, businessID, userID, &input.Filters) {
		return
	}
	result, err := h.svc.Query(c.Request.Context(), businessID, userID, c.Param("key"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if errors.Is(err, services.ErrReportScopeUnsupported) {
			statusCode = http.StatusForbidden
		} else if err.Error() == "at least one valid column is required" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// Export exports a report
// @Summary Export report
// @Description Exports a report in the specified format
// @Tags Reports
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param key path string true "Report key"
// @Param input body services.ReportExportInput true "Export parameters"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /reports/{key}/export [post]
func (h *ReportHandler) Export(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.ReportExportInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.applyReportScope(c, businessID, userID, &input.Filters) {
		return
	}
	result, err := h.svc.Export(c.Request.Context(), businessID, userID, c.Param("key"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if errors.Is(err, services.ErrReportScopeUnsupported) {
			statusCode = http.StatusForbidden
		} else if err.Error() == "unsupported export format" || err.Error() == "at least one valid column is required" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

// Dashboard returns dashboard data
// @Summary Dashboard
// @Description Returns dashboard data for the business
// @Tags Reports
// @Produce json
// @Security BearerAuth
// @Success 200 {object} interface{}
// @Failure 500 {object} map[string]string
// @Router /reports/dashboard [get]
func (h *ReportHandler) Dashboard(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	input, err := readReportQueryFromRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.applyReportScope(c, businessID, userID, &input.Filters) {
		return
	}
	result, err := h.svc.Dashboard(c.Request.Context(), businessID, input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, services.ErrReportScopeUnsupported) {
			statusCode = http.StatusForbidden
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetPreference retrieves a report preference
// @Summary Get report preference
// @Description Returns a saved report preference
// @Tags Reports
// @Produce json
// @Security BearerAuth
// @Param key path string true "Preference key"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /reports/preferences/{key} [get]
func (h *ReportHandler) GetPreference(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	result, err := h.svc.GetPreference(c.Request.Context(), businessID, userID, c.Param("key"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// SavePreference saves a report preference
// @Summary Save report preference
// @Description Saves a report preference
// @Tags Reports
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param key path string true "Preference key"
// @Param input body services.ReportPreferenceInput true "Preference data"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /reports/preferences/{key} [put]
func (h *ReportHandler) SavePreference(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.ReportPreferenceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if input.Columns != nil && len(input.Columns) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one valid column is required"})
		return
	}
	result, err := h.svc.SavePreference(c.Request.Context(), businessID, userID, c.Param("key"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "at least one valid column is required" || err.Error() == "unsupported export format" || err.Error() == "unsupported share mode" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateShare creates a report share
// @Summary Create report share
// @Description Creates a public share link for a report
// @Tags Reports
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param key path string true "Report key"
// @Param input body services.ReportShareInput true "Share parameters"
// @Success 201 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /reports/{key}/share [post]
func (h *ReportHandler) CreateShare(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	userID, ok := requireUserScope(c)
	if !ok {
		return
	}
	var input services.ReportShareInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.applyReportScope(c, businessID, userID, &input.Filters) {
		return
	}
	result, err := h.svc.CreateShare(c.Request.Context(), businessID, userID, c.Param("key"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if errors.Is(err, services.ErrReportScopeUnsupported) {
			statusCode = http.StatusForbidden
		} else if err.Error() == "unsupported share mode" || err.Error() == "expires_at must be in the future" || err.Error() == "at least one valid column is required" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ShareHistory returns report share history
// @Summary Report share history
// @Description Returns the history of report shares
// @Tags Reports
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /reports/shares/history [get]
func (h *ReportHandler) ShareHistory(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	page, limit := readPageLimit(c)
	items, total, err := h.svc.ListShares(c.Request.Context(), businessID, page, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":  items,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// PublicMetadata returns public metadata for a shared report
// @Summary Get public report metadata
// @Description Returns public metadata for a shared report
// @Tags Public
// @Produce json
// @Param token path string true "Share token"
// @Success 200 {object} interface{}
// @Failure 404 {object} map[string]string
// @Failure 410 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /public/report-shares/{token}/metadata [get]
func (h *ReportHandler) PublicMetadata(c *gin.Context) {
	result, err := h.svc.GetPublicMetadata(c.Request.Context(), c.Param("token"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		switch err.Error() {
		case "report share expired", "report share revoked":
			statusCode = http.StatusGone
		default:
			if isNotFoundErr(err) {
				statusCode = http.StatusNotFound
			}
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// PublicAccess provides public access to a shared report
// @Summary Access shared report
// @Description Provides access to a shared report using a token
// @Tags Public
// @Accept json
// @Produce json
// @Param token path string true "Share token"
// @Param input body services.ReportShareAccessInput false "Access parameters"
// @Success 200 {object} interface{}
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 410 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /public/report-shares/{token}/access [post]
func (h *ReportHandler) PublicAccess(c *gin.Context) {
	var input services.ReportShareAccessInput
	_ = c.ShouldBindJSON(&input)
	if input.Page == 0 || input.Limit == 0 {
		page, limit := readPageLimit(c)
		if input.Page == 0 {
			input.Page = page
		}
		if input.Limit == 0 {
			input.Limit = limit
		}
	}
	result, err := h.svc.AccessPublicShare(c.Request.Context(), c.Param("token"), input, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		statusCode := http.StatusInternalServerError
		switch err.Error() {
		case "passcode required", "invalid passcode":
			statusCode = http.StatusUnauthorized
		case "report share expired", "report share revoked":
			statusCode = http.StatusGone
		default:
			if isNotFoundErr(err) {
				statusCode = http.StatusNotFound
			}
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func readReportQueryFromRequest(c *gin.Context) (services.ReportQueryInput, error) {
	page, limit := readPageLimit(c)
	dateFrom, err := readDateQuery(c, "date_from")
	if err != nil {
		return services.ReportQueryInput{}, err
	}
	dateTo, err := readDateQuery(c, "date_to")
	if err != nil {
		return services.ReportQueryInput{}, err
	}
	includeCancelled := false
	if raw := c.Query("include_cancelled"); raw != "" {
		includeCancelled, err = strconv.ParseBool(raw)
		if err != nil {
			return services.ReportQueryInput{}, err
		}
	}
	return services.ReportQueryInput{
		Page:  page,
		Limit: limit,
		Filters: reporting.Filters{
			DateFrom:         dateFrom,
			DateTo:           dateTo,
			ProjectID:        c.Query("project_id"),
			WarehouseID:      c.Query("warehouse_id"),
			PartyID:          c.Query("party_id"),
			ProductID:        c.Query("product_id"),
			VariantID:        c.Query("variant_id"),
			CategoryID:       c.Query("category_id"),
			Search:           c.Query("search"),
			IncludeCancelled: includeCancelled,
		},
	}, nil
}

func (h *ReportHandler) applyReportScope(c *gin.Context, businessID, userID string, filters *reporting.Filters) bool {
	if filters == nil || h.inventory == nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "report scope unavailable"})
		return false
	}
	allBranches, branchIDs, ok := middleware.GetValidatedBranchScope(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "validated branch scope required"})
		return false
	}
	if !allBranches {
		filters.BranchScopeRestricted = true
		filters.AllowedBranchIDs = append([]string(nil), branchIDs...)
	} else {
		branchIDs = nil
	}

	if middleware.GetRole(c) == "admin" && allBranches {
		return true
	}
	allWarehouses, warehouseIDs, err := h.inventory.ReportWarehouseScope(c.Request.Context(), userID, businessID, branchIDs)
	if err != nil {
		h.log.Error("resolve report warehouse scope", "error", err, "business_id", businessID, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve report scope"})
		return false
	}
	if allWarehouses {
		return true
	}
	filters.WarehouseScopeRestricted = true
	filters.AllowedWarehouseIDs = append([]string(nil), warehouseIDs...)
	if filters.WarehouseID != "" && !containsString(warehouseIDs, filters.WarehouseID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied to this warehouse"})
		return false
	}
	return true
}

func readPageLimit(c *gin.Context) (int, int) {
	page := 1
	limit := 20
	if raw := c.Query("page"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	return page, limit
}

func readDateQuery(c *gin.Context, key string) (*time.Time, error) {
	raw := c.Query(key)
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
