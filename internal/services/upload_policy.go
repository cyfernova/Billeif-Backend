package services

import (
	"fmt"
	"strings"
)

const (
	MaxProfilePictureUploadBytes = int64(5 * 1024 * 1024)
	MaxBusinessLogoUploadBytes   = int64(5 * 1024 * 1024)
	MaxProductImageUploadBytes   = int64(5 * 1024 * 1024)
	MaxDriveAssetUploadBytes     = int64(25 * 1024 * 1024)
)

var allowedImageUploadContentTypes = map[string]struct{}{
	"image/gif":     {},
	"image/jpeg":    {},
	"image/png":     {},
	"image/svg+xml": {},
	"image/webp":    {},
}

func NormalizeImageUploadContentType(contentType string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(contentType))
	if _, ok := allowedImageUploadContentTypes[normalized]; !ok {
		return "", fmt.Errorf("unsupported content type; allowed: image/png, image/jpeg, image/gif, image/webp, image/svg+xml")
	}
	return normalized, nil
}

func validateUploadSize(kind string, sizeBytes, maxBytes int64) error {
	if sizeBytes <= 0 || sizeBytes > maxBytes {
		return fmt.Errorf("%s size must be between 1 byte and %d bytes", kind, maxBytes)
	}
	return nil
}
