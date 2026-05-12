package middleware

import (
	"strings"
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
			"client_ip", c.ClientIP(),
			"forwarded_for", c.GetHeader("X-Forwarded-For"),
			"path_length", len(c.Request.URL.Path),
			"raw_query_present", c.Request.URL.RawQuery != "",
			"query_key_count", queryKeyCount(c.Request.URL.RawQuery),
			"content_length", c.Request.ContentLength,
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
		if detected, exists := c.Get(securityDetectionKey); exists && detected == true {
			fields = append(fields, "security_detection", true)
			if categories, exists := c.Get(securityDetectionCategoriesKey); exists {
				fields = append(fields, "security_detection_categories", categories)
			}
			if severity, exists := c.Get(securityDetectionSeverityKey); exists {
				fields = append(fields, "security_detection_severity", severity)
			}
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

func queryKeyCount(rawQuery string) int {
	if rawQuery == "" {
		return 0
	}
	return strings.Count(rawQuery, "&") + 1
}
