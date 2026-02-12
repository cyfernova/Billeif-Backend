package middleware

import (
	"time"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

func Logger(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		requestID := GetRequestID(c)
		requestLog := log.With(
			"request_id", requestID,
			"method", c.Request.Method,
			"route", route,
			"path", c.Request.URL.Path,
			"client_ip", c.ClientIP(),
		)
		c.Request = c.Request.WithContext(logger.ToContext(c.Request.Context(), requestLog))

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		fields := []interface{}{
			"request_id", requestID,
			"status", status,
			"method", c.Request.Method,
			"route", route,
			"path", c.Request.URL.Path,
			"ip", c.ClientIP(),
			"duration_ms", latency.Milliseconds(),
			"user_agent", c.Request.UserAgent(),
		}

		if userID, exists := c.Get("user_id"); exists {
			fields = append(fields, "user_id", userID)
		}
		if businessID, exists := c.Get("business_id"); exists && businessID != "" {
			fields = append(fields, "business_id", businessID)
		}
		if role, exists := c.Get("role"); exists && role != "" {
			fields = append(fields, "role", role)
		}

		if len(c.Errors) > 0 {
			requestLog.Error("request completed with errors", append(fields, "errors", c.Errors.String())...)
			return
		}

		if status >= 500 {
			requestLog.Error("request completed", fields...)
		} else if status >= 400 {
			requestLog.Warn("request completed", fields...)
		} else {
			requestLog.Info("request completed", fields...)
		}
	}
}
