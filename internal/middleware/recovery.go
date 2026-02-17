package middleware

import (
	"net/http"
	"runtime/debug"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

func Recovery(log *logger.Logger) gin.HandlerFunc {
	return RecoveryWithOptions(log, false)
}

// RecoveryWithOptions creates a panic-recovery middleware. When production is
// true the full stack trace is omitted from logs to prevent code-structure
// leakage; only the error message and request ID are recorded.
func RecoveryWithOptions(log *logger.Logger, production bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				fields := []interface{}{
					"error", err,
					"request_id", GetRequestID(c),
					"path", c.Request.URL.Path,
					"method", c.Request.Method,
				}
				if !production {
					fields = append(fields, "stack", string(debug.Stack()))
				}
				log.Error("panic recovered", fields...)

				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":      "internal server error",
					"request_id": GetRequestID(c),
				})
			}
		}()
		c.Next()
	}
}
