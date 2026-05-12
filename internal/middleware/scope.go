package middleware

import "github.com/gin-gonic/gin"

// GetValidatedBusinessID returns the business ID explicitly validated by BusinessAuth middleware.
func GetValidatedBusinessID(c *gin.Context) string {
	if businessID, exists := c.Get(validatedBusinessIDKey); exists {
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

func GetValidatedBranchID(c *gin.Context) string {
	if branchID, exists := c.Get(validatedBranchIDKey); exists {
		if id, ok := branchID.(string); ok {
			return id
		}
	}
	return ""
}

func GetValidatedBranchScope(c *gin.Context) (bool, []string, bool) {
	allBranches, exists := c.Get(validatedBranchScopeAllKey)
	if !exists {
		return false, nil, false
	}
	all, ok := allBranches.(bool)
	if !ok {
		return false, nil, false
	}
	rawBranchIDs, _ := c.Get(validatedBranchScopeIDsKey)
	branchIDs, _ := rawBranchIDs.([]string)
	return all, append([]string(nil), branchIDs...), true
}
