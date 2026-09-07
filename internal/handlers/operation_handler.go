package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type OperationHandler struct {
	service *services.OperationService
	log     *logger.Logger
}

func NewOperationHandler(service *services.OperationService, log *logger.Logger) *OperationHandler {
	return &OperationHandler{service: service, log: log}
}

// ListBusiness returns the sanitized operational projection for the current business.
// @Summary List business operations
// @Description Returns a bounded, tenant-scoped operational projection. Provider references, queue identifiers, raw payloads, raw errors, secrets and topology are never returned.
// @Tags Operations
// @Produce json
// @Security BearerAuth
// @Param type query []string false "Operation type filter"
// @Param status query []string false "Normalized status filter"
// @Param limit query int false "Page size (1-100)" default(50)
// @Param cursor query string false "Opaque filter-bound page cursor"
// @Success 200 {object} services.OperationListResponse
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Router /operations [get]
func (h *OperationHandler) ListBusiness(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	input, err := operationListInput(c)
	if err != nil {
		writeOperationError(c, services.ErrInvalidOperationQuery)
		return
	}
	result, err := h.service.ListBusinessOperations(c.Request.Context(), businessID, input)
	if err != nil {
		writeOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetBusiness returns one sanitized operation for the current business.
// @Summary Get a business operation
// @Description Returns only the business-safe projection for the current tenant.
// @Tags Operations
// @Produce json
// @Security BearerAuth
// @Param operation_id path string true "Composite operation ID (type:UUID)"
// @Success 200 {object} services.OperationSummary
// @Failure 404 {object} map[string]string
// @Router /operations/{operation_id} [get]
func (h *OperationHandler) GetBusiness(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	result, err := h.service.GetBusinessOperation(c.Request.Context(), businessID, c.Param("operation_id"))
	if err != nil {
		writeOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// TimelineBusiness returns a sanitized operation timeline.
// @Summary Get a business operation timeline
// @Description Returns sanitized lifecycle and recovery events for the current tenant.
// @Tags Operations
// @Produce json
// @Security BearerAuth
// @Param operation_id path string true "Composite operation ID (type:UUID)"
// @Param limit query int false "Event limit (1-100)" default(50)
// @Success 200 {object} services.OperationTimeline
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /operations/{operation_id}/timeline [get]
func (h *OperationHandler) TimelineBusiness(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	limit, err := operationLimit(c, 50)
	if err != nil {
		writeOperationError(c, services.ErrInvalidOperationQuery)
		return
	}
	result, err := h.service.GetOperationTimeline(c.Request.Context(), businessID, c.Param("operation_id"), limit)
	if err != nil {
		writeOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// RecoverBusiness requests the single customer-safe recovery currently supported.
// @Summary Recover a business operation
// @Description Safely retries an exact failed versioned invoice render. Requires documents.manage, runtime capabilities, UUID idempotency/correlation identities and a durable audit.
// @Tags Operations
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param operation_id path string true "Composite operation ID (type:UUID)"
// @Param input body services.OperationRecoveryInput true "Recovery command"
// @Success 202 {object} services.OperationRecoveryResponse
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Router /operations/{operation_id}/recovery [post]
func (h *OperationHandler) RecoverBusiness(c *gin.Context) {
	businessID, ok := requireBusinessScope(c)
	if !ok {
		return
	}
	var input services.OperationRecoveryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeOperationError(c, services.ErrInvalidOperationRecovery)
		return
	}
	requestContextWithActor(c)
	result, err := h.service.RecoverBusinessOperation(c.Request.Context(), businessID, c.Param("operation_id"), input)
	if err != nil {
		writeOperationError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, result)
}

// GetOperator returns separately authorized, sanitized operator detail.
// @Summary Get operator operation detail
// @Description Requires the separately configured verified platform operator JWT group. Business owner/admin roles do not grant access.
// @Tags Operator Operations
// @Produce json
// @Security BearerAuth
// @Param business_id query string true "Original tenant business ID"
// @Param operation_id path string true "Composite operation ID (type:UUID)"
// @Success 200 {object} services.OperationOperatorDetail
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /operator/operations/{operation_id} [get]
func (h *OperationHandler) GetOperator(c *gin.Context) {
	result, err := h.service.GetOperatorOperation(c.Request.Context(), strings.TrimSpace(c.Query("business_id")), c.Param("operation_id"))
	if err != nil {
		writeOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// TimelineOperator returns a separately authorized sanitized timeline.
// @Summary Get operator operation timeline
// @Description Requires the configured verified platform operator JWT group and the original tenant ID.
// @Tags Operator Operations
// @Produce json
// @Security BearerAuth
// @Param business_id query string true "Original tenant business ID"
// @Param operation_id path string true "Composite operation ID (type:UUID)"
// @Param limit query int false "Event limit (1-100)" default(50)
// @Success 200 {object} services.OperationTimeline
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /operator/operations/{operation_id}/timeline [get]
func (h *OperationHandler) TimelineOperator(c *gin.Context) {
	limit, err := operationLimit(c, 50)
	if err != nil {
		writeOperationError(c, services.ErrInvalidOperationQuery)
		return
	}
	result, err := h.service.GetOperationTimeline(
		c.Request.Context(), strings.TrimSpace(c.Query("business_id")), c.Param("operation_id"), limit,
	)
	if err != nil {
		writeOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// RecoverOperator records a fail-closed high-risk recovery decision.
// @Summary Request operator recovery
// @Description Operator-only recovery boundary. High-risk actions remain blocked with step_up_required until scoped one-time step-up verification is delivered; all decisions are durable-audited.
// @Tags Operator Operations
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param business_id query string true "Original tenant business ID"
// @Param operation_id path string true "Composite operation ID (type:UUID)"
// @Param X-Step-Up-Token header string false "Task 5 scoped one-time step-up token"
// @Param input body services.OperationRecoveryInput true "Recovery command and reason"
// @Success 202 {object} services.OperationRecoveryResponse
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Failure 428 {object} map[string]string "step_up_required"
// @Failure 503 {object} map[string]string
// @Router /operator/operations/{operation_id}/recovery [post]
func (h *OperationHandler) RecoverOperator(c *gin.Context) {
	var input services.OperationRecoveryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		writeOperationError(c, services.ErrInvalidOperationRecovery)
		return
	}
	requestContextWithActor(c)
	result, err := h.service.RecoverOperatorOperation(
		c.Request.Context(), strings.TrimSpace(c.Query("business_id")), c.Param("operation_id"), input,
		strings.TrimSpace(c.GetHeader("X-Step-Up-Token")),
	)
	if err != nil {
		writeOperationError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, result)
}

func operationListInput(c *gin.Context) (services.OperationListInput, error) {
	limit, err := operationLimit(c, 50)
	if err != nil {
		return services.OperationListInput{}, err
	}
	statuses := splitOperationQuery(c.QueryArray("status"))
	statusValues := make([]services.OperationStatus, 0, len(statuses))
	for _, status := range statuses {
		statusValues = append(statusValues, services.OperationStatus(status))
	}
	return services.OperationListInput{
		Types: splitOperationQuery(c.QueryArray("type")), Statuses: statusValues,
		Limit: limit, Cursor: strings.TrimSpace(c.Query("cursor")),
	}, nil
}

func operationLimit(c *gin.Context, defaultValue int) (int, error) {
	raw := strings.TrimSpace(c.Query("limit"))
	if raw == "" {
		return defaultValue, nil
	}
	return strconv.Atoi(raw)
}

func splitOperationQuery(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				result = append(result, part)
			}
		}
	}
	return result
}

func writeOperationError(c *gin.Context, err error) {
	status, code, message := http.StatusInternalServerError, "operation_unavailable", "operation request unavailable"
	switch {
	case errors.Is(err, services.ErrInvalidOperationQuery):
		status, code, message = http.StatusBadRequest, "invalid_operation_query", "invalid operation query"
	case errors.Is(err, services.ErrInvalidOperationRecovery):
		status, code, message = http.StatusBadRequest, "invalid_recovery_request", "invalid recovery request"
	case errors.Is(err, services.ErrOperationNotFound):
		status, code, message = http.StatusNotFound, "operation_not_found", "operation not found"
	case errors.Is(err, services.ErrPermissionDenied):
		status, code, message = http.StatusForbidden, "operation_access_denied", "operation access denied"
	case errors.Is(err, services.ErrUnsafeOperationReplay):
		status, code, message = http.StatusConflict, services.OperationCodeUnsafeReplay, "unsafe operation replay"
	case errors.Is(err, services.ErrUnsupportedOperationRecovery):
		status, code, message = http.StatusUnprocessableEntity, services.OperationCodeUnsupportedRecovery, "operation recovery is unsupported"
	case errors.Is(err, services.ErrOperationStepUpRequired):
		status, code, message = http.StatusPreconditionRequired, services.OperationCodeStepUpRequired, "operation recovery step-up is required"
	case errors.Is(err, services.ErrOperationAuditUnavailable):
		status, code, message = http.StatusServiceUnavailable, "recovery_audit_unavailable", "operation recovery audit is unavailable"
	}
	c.JSON(status, gin.H{"code": code, "error": message})
}
