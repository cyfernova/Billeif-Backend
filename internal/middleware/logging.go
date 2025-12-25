package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/cyfernova/invoice-backend/pkg/logger"
)

// Logging returns a middleware that logs HTTP requests
func Logging(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		method := c.Request.Method
		ip := c.ClientIP()
		requestID := GetRequestID(c)

		log.Info().
			Str("request_id", requestID).
			Str("method", method).
			Str("path", path).
			Str("query", query).
			Int("status", status).
			Dur("latency", latency).
			Str("ip", ip).
			Str("user_agent", c.Request.UserAgent()).
			Msg("HTTP request")
	}
}
