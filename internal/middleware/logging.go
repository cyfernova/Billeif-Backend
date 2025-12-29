package middleware

import (
	"time"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

func Logger(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		fields := []interface{}{
			"status", status,
			"method", c.Request.Method,
			"path", path,
			"query", query,
			"ip", c.ClientIP(),
			"latency", latency.String(),
			"user_agent", c.Request.UserAgent(),
			"request_id", GetRequestID(c),
		}

		if userID, exists := c.Get("user_id"); exists {
			fields = append(fields, "user_id", userID)
		}

		if len(c.Errors) > 0 {
			log.Error("request completed with errors", append(fields, "errors", c.Errors.String())...)
			return
		}

		if status >= 500 {
			log.Error("request completed", fields...)
		} else if status >= 400 {
			log.Warn("request completed", fields...)
		} else {
			log.Info("request completed", fields...)
		}
	}
}
