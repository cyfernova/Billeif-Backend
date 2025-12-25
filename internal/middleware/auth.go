package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/cyfernova/invoice-backend/internal/config"
	"github.com/cyfernova/invoice-backend/internal/utils"
)

const (
	UserIDKey    = "user_id"
	UserEmailKey = "user_email"
	UserGroupsKey = "user_groups"
	UserRoleKey  = "user_role"
)

// AuthMiddleware handles JWT authentication
type AuthMiddleware struct {
	jwtSecret string
}

// NewAuthMiddleware creates a new auth middleware
func NewAuthMiddleware(cfg config.JWTConfig) *AuthMiddleware {
	return &AuthMiddleware{
		jwtSecret: cfg.Secret,
	}
}

// Claims represents JWT claims
type Claims struct {
	Sub    string   `json:"sub"`
	Email  string   `json:"email"`
	Groups []string `json:"cognito:groups"`
	jwt.RegisteredClaims
}

// RequireAuth returns a middleware that requires authentication
func (m *AuthMiddleware) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, utils.Response{
				Success: false,
				Error: &utils.ErrorInfo{
					Code:    "UNAUTHORIZED",
					Message: "Missing authorization header",
				},
			})
			c.Abort()
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, utils.Response{
				Success: false,
				Error: &utils.ErrorInfo{
					Code:    "UNAUTHORIZED",
					Message: "Invalid authorization header format",
				},
			})
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(m.jwtSecret), nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, utils.Response{
				Success: false,
				Error: &utils.ErrorInfo{
					Code:    "UNAUTHORIZED",
					Message: "Invalid or expired token",
				},
			})
			c.Abort()
			return
		}

		// Store user info in context
		c.Set(UserIDKey, claims.Sub)
		c.Set(UserEmailKey, claims.Email)
		c.Set(UserGroupsKey, claims.Groups)

		c.Next()
	}
}

// OptionalAuth returns a middleware that optionally authenticates
func (m *AuthMiddleware) OptionalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Next()
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.Next()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(m.jwtSecret), nil
		})

		if err == nil && token.Valid {
			c.Set(UserIDKey, claims.Sub)
			c.Set(UserEmailKey, claims.Email)
			c.Set(UserGroupsKey, claims.Groups)
		}

		c.Next()
	}
}

// GetUser retrieves user info from context
func GetUser(c *gin.Context) (string, string, []string) {
	userID, _ := c.Get(UserIDKey)
	email, _ := c.Get(UserEmailKey)
	groups, _ := c.Get(UserGroupsKey)

	if userID == nil {
		return "", "", nil
	}

	uid := userID.(string)
	em := ""
	if email != nil {
		em = email.(string)
	}

	gr := []string{}
	if groups != nil {
		gr = groups.([]string)
	}

	return uid, em, gr
}

// GetUserID retrieves just the user ID from context
func GetUserID(c *gin.Context) string {
	userID, _, _ := GetUser(c)
	return userID
}

// GetUserEmail retrieves just the user email from context
func GetUserEmail(c *gin.Context) string {
	_, email, _ := GetUser(c)
	return email
}

// GetUserGroups retrieves just the user groups from context
func GetUserGroups(c *gin.Context) []string {
	_, _, groups := GetUser(c)
	return groups
}
