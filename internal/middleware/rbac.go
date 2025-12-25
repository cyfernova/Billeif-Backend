package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/cyfernova/invoice-backend/internal/models"
	"github.com/cyfernova/invoice-backend/internal/utils"
)

// RequireRole checks if user has any of the required roles
func RequireRole(roles ...models.UserRole) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, _, groups := GetUser(c)

		// Map Cognito groups to app roles
		userRole := models.RoleUser
		for _, group := range groups {
			switch group {
			case "admin":
				userRole = models.RoleAdmin
			case "accountant":
				userRole = models.RoleAccountant
			case "viewer":
				userRole = models.RoleViewer
			}
		}

		hasPermission := false
		for _, role := range roles {
			if userRole == role || userRole == models.RoleAdmin {
				hasPermission = true
				break
			}
		}

		if !hasPermission {
			c.JSON(http.StatusForbidden, utils.Response{
				Success: false,
				Error: &utils.ErrorInfo{
					Code:    "FORBIDDEN",
					Message: "Insufficient permissions",
				},
			})
			c.Abort()
			return
		}

		c.Set(UserRoleKey, userRole)
		c.Next()
	}
}

// RequireAdmin checks if user is admin
func RequireAdmin() gin.HandlerFunc {
	return RequireRole(models.RoleAdmin)
}

// RequireAccountant checks if user is accountant or admin
func RequireAccountant() gin.HandlerFunc {
	return RequireRole(models.RoleAccountant, models.RoleAdmin)
}

// RequireViewer checks if user is viewer, accountant, or admin
func RequireViewer() gin.HandlerFunc {
	return RequireRole(models.RoleViewer, models.RoleAccountant, models.RoleAdmin)
}

// GetUserRole retrieves the user role from context
func GetUserRole(c *gin.Context) models.UserRole {
	if role, exists := c.Get(UserRoleKey); exists {
		if r, ok := role.(models.UserRole); ok {
			return r
		}
	}
	return models.RoleUser
}

// HasPermission checks if current user has a specific permission
func HasPermission(c *gin.Context, permission string) bool {
	role := GetUserRole(c)
	return role.HasPermission(permission)
}
