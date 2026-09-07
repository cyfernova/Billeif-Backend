package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RequirePlatformOperator accepts only the separately configured, verified JWT
// group populated by Auth. Business roles are deliberately ignored.
func RequirePlatformOperator(configuredGroup string) gin.HandlerFunc {
	configuredGroup = strings.TrimSpace(configuredGroup)
	return func(c *gin.Context) {
		if configuredGroup == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code": "operator_not_configured", "error": "operator access is not configured",
			})
			return
		}
		for _, group := range GetGroups(c) {
			if strings.TrimSpace(group) == configuredGroup {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"code": "operator_access_denied", "error": "operator access denied",
		})
	}
}
