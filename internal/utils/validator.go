package utils

import "errors"

func ValidateEmail(email string) bool {
	return email != "" && contains(email, "@") && contains(email, ".")
}

func ValidateUUID(id string) bool {
	return id != "" && len(id) == 36
}

func ValidateRequired(field, value string) error {
	if value == "" {
		return errors.New(field + " is required")
	}
	return nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
