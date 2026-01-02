package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"invoice-backend/pkg/logger"
	pkgsentry "invoice-backend/pkg/sentry"

	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
)

// SentryMiddleware returns the Sentry middleware for Gin
// This captures request context and enables transaction tracing
func SentryMiddleware() gin.HandlerFunc {
	return sentrygin.New(sentrygin.Options{
		Repanic:         true,  // Re-panic after capturing so our recovery middleware can handle it
		WaitForDelivery: false, // Don't block responses waiting for Sentry
		Timeout:         2 * time.Second,
	})
}

// SentryRecovery returns a recovery middleware that captures panics to Sentry
// This should be used instead of or in addition to the standard Recovery middleware
func SentryRecovery(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				requestID := GetRequestID(c)
				stackTrace := string(debug.Stack())

				// Log the panic locally
				log.Error("panic recovered",
					"error", err,
					"stack", stackTrace,
					"request_id", requestID,
					"path", c.Request.URL.Path,
					"method", c.Request.Method,
				)

				// Capture to Sentry with detailed context
				capturePanicToSentry(c, err, requestID, stackTrace)

				// Respond with error
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":      "internal server error",
					"request_id": requestID,
				})
			}
		}()
		c.Next()
	}
}

// capturePanicToSentry captures a panic to Sentry with full context
func capturePanicToSentry(c *gin.Context, err interface{}, requestID string, stackTrace string) {
	// Get the hub from context (set by SentryMiddleware)
	if hub := sentrygin.GetHubFromContext(c); hub != nil {
		hub.WithScope(func(scope *sentry.Scope) {
			// Set level to fatal for panics
			scope.SetLevel(sentry.LevelFatal)

			// Add request context
			scope.SetTag("panic", "true")
			scope.SetTag("request_id", requestID)
			scope.SetTag("path", c.Request.URL.Path)
			scope.SetTag("method", c.Request.Method)
			scope.SetTag("client_ip", c.ClientIP())

			// Add request details
			scope.SetExtra("stack_trace", stackTrace)
			scope.SetExtra("request_headers", getRequestHeaders(c))
			scope.SetExtra("query_params", c.Request.URL.Query())

			// Set request context
			scope.SetRequest(c.Request)

			// Add user context if available
			if userID, exists := c.Get("user_id"); exists {
				scope.SetUser(sentry.User{
					ID: fmt.Sprintf("%v", userID),
				})
			}

			// Capture the panic
			if e, ok := err.(error); ok {
				hub.CaptureException(e)
			} else {
				hub.CaptureMessage(fmt.Sprintf("Panic: %v", err))
			}
		})
	} else {
		// Fallback to global capture if hub is not available
		pkgsentry.CapturePanic(err, requestID, c.Request.URL.Path, c.Request.Method)
	}
}

// SentryErrorHandler captures errors to Sentry with detailed context
// Use this in handlers when you want to report errors but not panic
func SentryErrorHandler(c *gin.Context, err error, tags map[string]string, extras map[string]interface{}) {
	if err == nil {
		return
	}

	// Get the hub from context
	if hub := sentrygin.GetHubFromContext(c); hub != nil {
		hub.WithScope(func(scope *sentry.Scope) {
			// Add default tags
			scope.SetTag("request_id", GetRequestID(c))
			scope.SetTag("path", c.Request.URL.Path)
			scope.SetTag("method", c.Request.Method)

			// Add custom tags
			for key, value := range tags {
				scope.SetTag(key, value)
			}

			// Add extra context
			for key, value := range extras {
				scope.SetExtra(key, value)
			}

			// Set request context
			scope.SetRequest(c.Request)

			// Add user context if available
			if userID, exists := c.Get("user_id"); exists {
				scope.SetUser(sentry.User{
					ID: fmt.Sprintf("%v", userID),
				})
			}

			hub.CaptureException(err)
		})
	} else {
		// Fallback to package-level capture
		pkgsentry.CaptureError(err, tags, extras)
	}
}

// AddSentryBreadcrumb adds a breadcrumb to the current Sentry scope
func AddSentryBreadcrumb(c *gin.Context, category, message string, level sentry.Level, data map[string]interface{}) {
	if hub := sentrygin.GetHubFromContext(c); hub != nil {
		hub.AddBreadcrumb(&sentry.Breadcrumb{
			Category:  category,
			Message:   message,
			Level:     level,
			Data:      data,
			Timestamp: time.Now(),
		}, nil)
	}
}

// SetSentryUser sets the user context for the current request
func SetSentryUser(c *gin.Context, userID, email, username string) {
	if hub := sentrygin.GetHubFromContext(c); hub != nil {
		hub.Scope().SetUser(sentry.User{
			ID:       userID,
			Email:    email,
			Username: username,
		})
	}
}

// getRequestHeaders extracts safe headers from the request
func getRequestHeaders(c *gin.Context) map[string]string {
	headers := make(map[string]string)
	safeHeaders := []string{
		"Content-Type",
		"Accept",
		"User-Agent",
		"Referer",
		"Origin",
		"X-Request-ID",
		"X-Forwarded-For",
		"X-Real-IP",
	}

	for _, header := range safeHeaders {
		if value := c.GetHeader(header); value != "" {
			headers[header] = value
		}
	}

	return headers
}
