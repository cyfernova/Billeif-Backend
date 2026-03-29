package handlers

import (
	"net/http"
	"strconv"
	"time"

	"invoice-backend/internal/reporting"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type ReportHandler struct {
	svc *services.ReportService
	log *logger.Logger
}

func NewReportHandler(svc *services.ReportService, log *logger.Logger) *ReportHandler {
	return &ReportHandler{svc: svc, log: log}
}

func (h *ReportHandler) Catalog(c *gin.Context) {
	if _, ok := requireBusinessScope(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": h.svc.Catalog()})
}

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
	result, err := h.svc.Query(c.Request.Context(), businessID, userID, c.Param("key"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "at least one valid column is required" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

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
	result, err := h.svc.Export(c.Request.Context(), businessID, userID, c.Param("key"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "unsupported export format" || err.Error() == "at least one valid column is required" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *ReportHandler) Dashboard(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	input, err := readReportQueryFromRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.svc.Dashboard(c.Request.Context(), businessID, input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

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
	result, err := h.svc.SavePreference(c.Request.Context(), businessID, userID, c.Param("key"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "at least one valid column is required" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

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
	result, err := h.svc.CreateShare(c.Request.Context(), businessID, userID, c.Param("key"), input)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if isNotFoundErr(err) {
			statusCode = http.StatusNotFound
		} else if err.Error() == "unsupported share mode" || err.Error() == "expires_at must be in the future" || err.Error() == "at least one valid column is required" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, result)
}

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
