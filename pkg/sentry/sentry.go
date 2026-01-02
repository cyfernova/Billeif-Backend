package sentry

import (
	"fmt"
	"runtime"
	"time"

	"github.com/getsentry/sentry-go"
)

// Config holds Sentry configuration options
type Config struct {
	DSN              string
	Environment      string
	Release          string
	Debug            bool
	SampleRate       float64
	TracesSampleRate float64
	EnableTracing    bool
}

// Init initializes the Sentry SDK with production-grade configuration
func Init(cfg Config) error {
	if cfg.DSN == "" {
		return nil // Sentry is disabled if no DSN is provided
	}

	// Set default sample rates for production
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 1.0 // Capture 100% of errors
	}
	if cfg.TracesSampleRate == 0 {
		cfg.TracesSampleRate = 0.2 // Sample 20% of transactions for performance monitoring
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              cfg.DSN,
		Environment:      cfg.Environment,
		Release:          cfg.Release,
		Debug:            cfg.Debug,
		SampleRate:       cfg.SampleRate,
		TracesSampleRate: cfg.TracesSampleRate,
		EnableTracing:    cfg.EnableTracing,

		// Production-grade error handling
		AttachStacktrace: true,

		// Configure server name for better identification
		ServerName: getServerName(),

		// Add before send hook for detailed error enrichment
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			// Enrich event with additional context
			enrichEvent(event)
			return event
		},

		// Add before send transaction hook for performance monitoring
		BeforeSendTransaction: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			return event
		},
	})

	if err != nil {
		return fmt.Errorf("failed to initialize Sentry: %w", err)
	}

	return nil
}

// Flush flushes any buffered events before program exits
func Flush(timeout time.Duration) {
	sentry.Flush(timeout)
}

// CaptureError captures an error with additional context
func CaptureError(err error, tags map[string]string, extras map[string]interface{}) {
	if err == nil {
		return
	}

	sentry.WithScope(func(scope *sentry.Scope) {
		// Add tags for filtering in Sentry UI
		for key, value := range tags {
			scope.SetTag(key, value)
		}

		// Add extra context for debugging
		for key, value := range extras {
			scope.SetExtra(key, value)
		}

		sentry.CaptureException(err)
	})
}

// CaptureMessage captures a message with a specific level
func CaptureMessage(message string, level sentry.Level, tags map[string]string, extras map[string]interface{}) {
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(level)

		for key, value := range tags {
			scope.SetTag(key, value)
		}

		for key, value := range extras {
			scope.SetExtra(key, value)
		}

		sentry.CaptureMessage(message)
	})
}

// CapturePanic captures a panic with stack trace
func CapturePanic(r interface{}, requestID string, path string, method string) {
	if r == nil {
		return
	}

	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelFatal)
		scope.SetTag("panic", "true")
		scope.SetTag("request_id", requestID)
		scope.SetTag("path", path)
		scope.SetTag("method", method)

		// Get stack trace
		stackBuf := make([]byte, 4096)
		stackSize := runtime.Stack(stackBuf, false)
		scope.SetExtra("stack_trace", string(stackBuf[:stackSize]))

		// Capture as either error or message
		if err, ok := r.(error); ok {
			sentry.CaptureException(err)
		} else {
			sentry.CaptureMessage(fmt.Sprintf("Panic: %v", r))
		}
	})
}

// SetUser sets the current user context
func SetUser(userID, email, username string) {
	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetUser(sentry.User{
			ID:       userID,
			Email:    email,
			Username: username,
		})
	})
}

// ClearUser clears the current user context
func ClearUser() {
	sentry.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetUser(sentry.User{})
	})
}

// AddBreadcrumb adds a breadcrumb for debugging
func AddBreadcrumb(category, message string, level sentry.Level, data map[string]interface{}) {
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category:  category,
		Message:   message,
		Level:     level,
		Data:      data,
		Timestamp: time.Now(),
	})
}

// StartSpan starts a new span for performance monitoring
// Note: When using the Gin middleware, spans are automatically created
// Use this for manual span creation in non-HTTP contexts
func StartSpan(parentSpan *sentry.Span, operation, description string) *sentry.Span {
	if parentSpan == nil {
		return nil
	}
	return parentSpan.StartChild(operation, sentry.WithDescription(description))
}

// enrichEvent adds additional context to the event
func enrichEvent(event *sentry.Event) {
	// Add Go version
	event.Contexts["runtime"] = sentry.Context{
		"name":    "Go",
		"version": runtime.Version(),
	}

	// Add OS and architecture info
	event.Contexts["os"] = sentry.Context{
		"name": runtime.GOOS,
	}
	event.Contexts["device"] = sentry.Context{
		"arch": runtime.GOARCH,
	}

	// Add goroutine count for debugging concurrency issues
	event.Extra["goroutine_count"] = runtime.NumGoroutine()

	// Add memory stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	event.Extra["heap_alloc_mb"] = memStats.HeapAlloc / 1024 / 1024
	event.Extra["heap_objects"] = memStats.HeapObjects
}

// getServerName returns a unique server identifier
func getServerName() string {
	// In production, this could be fetched from environment variables
	// like hostname, pod name, or instance ID
	return fmt.Sprintf("go-%s-%s", runtime.GOOS, runtime.GOARCH)
}
