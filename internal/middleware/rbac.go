package middleware

import (
	"net/http"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

func RequireRole(roles ...string) gin.HandlerFunc {

	return func(c *gin.Context) {
		log := logger.FromContext(c.Request.Context()).Named("rbac")
		userRole := GetRole(c)
		groups := GetGroups(c)

		for _, role := range roles {
			if userRole == role {
				c.Next()
				return
			}
			for _, group := range groups {
				if group == role {
					c.Next()
					return
				}
			}
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "insufficient permissions",
		})
		log.Warn("role access denied", "required_roles", roles, "user_role", userRole, "groups", groups)
	}
}

func RequireBusinessAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		log := logger.FromContext(c.Request.Context()).Named("rbac")
		userRole := GetRole(c)
		if userRole == "admin" {
			c.Next()
			return
		}

		businessIDParam := c.Param("business_id")
		if businessIDParam == "" {
			businessIDParam = c.Query("business_id")
		}
		if businessIDParam == "" {
			c.Next()
			return
		}

		userBusinessID := GetBusinessID(c)
		if userBusinessID != businessIDParam {
			log.Warn("business scope access denied",
				"user_business_id", userBusinessID,
				"requested_business_id", businessIDParam,
				"user_role", userRole,
			)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "access denied to this business",
			})
			return
		}

		c.Next()
	}
}

func RequirePermission(authSvc *services.BusinessAuthService, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		log := logger.FromContext(c.Request.Context()).Named("rbac")
		userID := GetUserID(c)
		businessID := GetEffectiveBusinessID(c)
		if userID == "" || businessID == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "business scope required"})
			return
		}
		if authSvc == nil || !authSvc.UserHasPermission(c.Request.Context(), userID, businessID, permission) {
			log.Warn("permission access denied", "permission", permission, "user_id", userID, "business_id", businessID)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			return
		}
		c.Next()
	}
}
