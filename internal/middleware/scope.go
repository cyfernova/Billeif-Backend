package middleware

import "github.com/gin-gonic/gin"

// GetValidatedBusinessID returns the business ID explicitly validated by BusinessAuth middleware.
func GetValidatedBusinessID(c *gin.Context) string {
	if businessID, exists := c.Get("validated_business_id"); exists {
		if id, ok := businessID.(string); ok {
			return id
		}
	}
	return ""
}

// GetEffectiveBusinessID returns the strongest available business scope for the request.
// Priority: validated_business_id (middleware-enforced) -> business_id claim.
func GetEffectiveBusinessID(c *gin.Context) string {
	if id := GetValidatedBusinessID(c); id != "" {
		return id
	}
	return GetBusinessID(c)
}
