package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// HealthChecker is an interface for health checking
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}

// HealthHandler handles health check endpoints
type HealthHandler struct {
	*Handler
	db       HealthChecker
	cognito  HealthChecker
	dynamodb HealthChecker
	s3       HealthChecker
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(db, cognito, dynamodb, s3 HealthChecker) *HealthHandler {
	return &HealthHandler{
		Handler:  &Handler{},
		db:       db,
		cognito:  cognito,
		dynamodb: dynamodb,
		s3:       s3,
	}
}

// HealthStatus represents health status
type HealthStatus struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Services  map[string]string `json:"services,omitempty"`
}

// Liveness handles liveness probe
// @Summary Liveness probe
// @Description Check if service is running
// @Tags health
// @Produce json
// @Success 200 {object} HealthStatus
// @Router /health [get]
func (h *HealthHandler) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, HealthStatus{
		Status:    "healthy",
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

// Readiness handles readiness probe
// @Summary Readiness probe
// @Description Check if service is ready to accept traffic
// @Tags health
// @Produce json
// @Success 200 {object} HealthStatus
// @Failure 503 {object} HealthStatus
// @Router /readiness [get]
func (h *HealthHandler) Readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	services := make(map[string]string)
	overallStatus := "healthy"

	// Check Database
	if h.db != nil {
		if err := h.db.HealthCheck(ctx); err != nil {
			services["database"] = "unhealthy"
			overallStatus = "unhealthy"
		} else {
			services["database"] = "healthy"
		}
	}

	// Check Cognito
	if h.cognito != nil {
		if err := h.cognito.HealthCheck(ctx); err != nil {
			services["cognito"] = "unhealthy"
			overallStatus = "unhealthy"
		} else {
			services["cognito"] = "healthy"
		}
	}

	// Check DynamoDB
	if h.dynamodb != nil {
		if err := h.dynamodb.HealthCheck(ctx); err != nil {
			services["dynamodb"] = "unhealthy"
			overallStatus = "unhealthy"
		} else {
			services["dynamodb"] = "healthy"
		}
	}

	// Check S3
	if h.s3 != nil {
		if err := h.s3.HealthCheck(ctx); err != nil {
			services["s3"] = "unhealthy"
			overallStatus = "unhealthy"
		} else {
			services["s3"] = "healthy"
		}
	}

	status := http.StatusOK
	if overallStatus != "healthy" {
		status = http.StatusServiceUnavailable
	}

	c.JSON(status, HealthStatus{
		Status:    overallStatus,
		Timestamp: time.Now().Format(time.RFC3339),
		Services:  services,
	})
}
