package handlers

import (
	"errors"
	"net/http"

	"invoice-backend/internal/services"

	"github.com/gin-gonic/gin"
)

func writeSubscriptionControlError(c *gin.Context, err error) bool {
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
