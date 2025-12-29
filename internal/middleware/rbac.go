package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
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
	}
}

func RequireBusinessAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
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
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "access denied to this business",
			})
			return
		}

		c.Next()
	}
}
