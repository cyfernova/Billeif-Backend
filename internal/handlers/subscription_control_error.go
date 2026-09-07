package handlers

import (
	"errors"
	"net/http"
	"time"

	"invoice-backend/internal/services"

	"github.com/gin-gonic/gin"
)

// CapabilityMutationError is the stable customer contract returned when a
// governed mutation is rejected by runtime capability preflight.
type CapabilityMutationError struct {
	Code        string                   `json:"code"`
	Error       string                   `json:"error"`
	Capability  services.CapabilityKey   `json:"capability"`
	State       services.CapabilityState `json:"state"`
	ReasonCode  string                   `json:"reason_code"`
	SetupAction string                   `json:"setup_action,omitempty"`
	RetryAt     *time.Time               `json:"retry_at,omitempty"`
}

func writeSubscriptionControlError(c *gin.Context, err error) bool {
	var capabilityErr *services.CapabilityUnavailableError
	if errors.As(err, &capabilityErr) {
		status := http.StatusForbidden
		switch capabilityErr.State {
		case services.CapabilityStateQuotaExhausted:
			status = http.StatusTooManyRequests
		case services.CapabilityStateTemporarilyUnavailable, services.CapabilityStateUnknown:
			status = http.StatusServiceUnavailable
		case services.CapabilityStateUnsupported, services.CapabilityStateUnsupportedPlatform:
			status = http.StatusUnprocessableEntity
		}
		payload := gin.H{
			"code":        capabilityErr.Code,
			"error":       capabilityErr.Error(),
			"capability":  capabilityErr.Capability,
			"state":       capabilityErr.State,
			"reason_code": capabilityErr.ReasonCode,
		}
		if capabilityErr.SetupAction != "" {
			payload["setup_action"] = capabilityErr.SetupAction
		}
		if capabilityErr.RetryAt != nil {
			payload["retry_at"] = capabilityErr.RetryAt.UTC().Format(time.RFC3339)
		}
		c.JSON(status, payload)
		return true
	}

	var quotaErr *services.QuotaExceededError
	if errors.As(err, &quotaErr) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"code":    quotaErr.Code,
			"error":   quotaErr.Error(),
			"feature": quotaErr.Feature,
			"limit":   quotaErr.Limit,
			"used":    quotaErr.Used,
			"plan_id": quotaErr.PlanID,
		})
		return true
	}

	var featureErr *services.FeatureUnavailableError
	if errors.As(err, &featureErr) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    featureErr.Code,
			"error":   featureErr.Error(),
			"feature": featureErr.Feature,
			"plan_id": featureErr.PlanID,
		})
		return true
	}
	return false
}
