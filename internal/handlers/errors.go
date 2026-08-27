package handlers

import (
	"errors"
	"strings"

	"invoice-backend/internal/services"
)

func isPermissionDeniedErr(err error) bool {
	return errors.Is(err, services.ErrPermissionDenied)
}

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}
