package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/cyfernova/invoice-backend/internal/utils"
	"github.com/cyfernova/invoice-backend/pkg/logger"
)

// Recovery returns a middleware that recovers from panics
func Recovery(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().
					Str("path", c.Request.URL.Path).
					Str("method", c.Request.Method).
					Str("stack", string(debug.Stack())).
					Interface("panic", r).
					Msg("Panic recovered")

				c.JSON(http.StatusInternalServerError, utils.Response{
					Success: false,
					Error: &utils.ErrorInfo{
						Code:    "INTERNAL_SERVER_ERROR",
						Message: "An unexpected error occurred",
					},
				})
				c.Abort()
			}
		}()
		c.Next()
	}
}
