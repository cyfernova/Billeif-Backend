package handlers

import (
	"invoice-backend/internal/middleware"
	"invoice-backend/internal/services"

	"github.com/gin-gonic/gin"
)

func requestContextWithActor(c *gin.Context) {
	ctx := services.ContextWithActor(c.Request.Context(), services.ActorContext{
		UserID:    middleware.GetUserID(c),
		Role:      middleware.GetRole(c),
		RequestID: middleware.GetRequestID(c),
		IPAddress: c.ClientIP(),
	})
	c.Request = c.Request.WithContext(ctx)
}
// 