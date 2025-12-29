package http

import (
	"time"

	"net/http"

	"github.com/gin-gonic/gin"
)

type HealthResponse struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Services  map[string]string `json:"services"`
}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

type HealthHandler struct {
	dynamoDBHealthy bool
	s3Healthy       bool
	sesHealthy      bool
	snsHealthy      bool
	sqsHealthy      bool
}

func (h *HealthHandler) Check(c *gin.Context) {
	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().Format(time.RFC3339),
		Services: map[string]string{
			"dynamodb": "unknown",
			"s3":       "unknown",
			"ses":      "unknown",
			"sns":      "unknown",
			"sqs":      "unknown",
		},
	}

	if h.dynamoDBHealthy {
		response.Services["dynamodb"] = "healthy"
	} else {
		response.Services["dynamodb"] = "unhealthy"
		response.Status = "degraded"
	}

	if h.s3Healthy {
		response.Services["s3"] = "healthy"
	} else {
		response.Services["s3"] = "unhealthy"
		response.Status = "degraded"
	}

	if h.sesHealthy {
		response.Services["ses"] = "healthy"
	} else {
		response.Services["ses"] = "unhealthy"
		response.Status = "degraded"
	}

	if h.snsHealthy {
		response.Services["sns"] = "healthy"
	} else {
		response.Services["sns"] = "unhealthy"
		response.Status = "degraded"
	}

	if h.sqsHealthy {
		response.Services["sqs"] = "healthy"
	} else {
		response.Services["sqs"] = "unhealthy"
		response.Status = "degraded"
	}

	if response.Status == "degraded" {
		c.JSON(http.StatusServiceUnavailable, response)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *HealthHandler) SetDynamoDBHealth(healthy bool) {
	h.dynamoDBHealthy = healthy
}

func (h *HealthHandler) SetS3Health(healthy bool) {
	h.s3Healthy = healthy
}

func (h *HealthHandler) SetSESHealth(healthy bool) {
	h.sesHealthy = healthy
}

func (h *HealthHandler) SetSNSHealth(healthy bool) {
	h.snsHealthy = healthy
}

func (h *HealthHandler) SetSQSHealth(healthy bool) {
	h.sqsHealthy = healthy
}
