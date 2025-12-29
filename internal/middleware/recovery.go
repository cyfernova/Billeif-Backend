package middleware

import (
	"net/http"
	"runtime/debug"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

func Recovery(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				log.Error("panic recovered",
					"error", err,
					"stack", string(debug.Stack()),
					"request_id", GetRequestID(c),
					"path", c.Request.URL.Path,
					"method", c.Request.Method,
				)

				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":      "internal server error",
					"request_id": GetRequestID(c),
				})
			}
		}()
		c.Next()
	}
}
