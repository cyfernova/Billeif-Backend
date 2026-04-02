package services

import "strings"

const CurrentSubscriptionCatalogVersion = "swipe-v1"

func normalizePlanCode(plan, planCode string) string {
	if code := strings.ToLower(strings.TrimSpace(planCode)); code != "" {
		return code
	}
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "starter":
		return "pro"
	case "professional":
		return "rise"
	case "enterprise":
		return "biz"
	case "free":
		return "free"
	default:
		return "free"
	}
}

func normalizeCatalogVersion(value string) string {
	if strings.TrimSpace(value) == "" {
		return CurrentSubscriptionCatalogVersion
	}
	return value
}
