package services

import (
	"fmt"
	"path"
	"strings"
)

func tenantArtifactObjectKey(prefix, businessID, namespaceID, fileName string) (string, error) {
	prefix = strings.Trim(path.Clean(strings.TrimSpace(prefix)), "/")
	businessID = strings.TrimSpace(businessID)
	namespaceID = strings.TrimSpace(namespaceID)
	if prefix == "" || prefix == "." || strings.Contains(prefix, "..") {
		return "", fmt.Errorf("artifact prefix is required")
	}
	if businessID == "" || strings.ContainsAny(businessID, "/\\") {
		return "", fmt.Errorf("artifact business scope is invalid")
	}
	if namespaceID == "" || strings.ContainsAny(namespaceID, "/\\") {
		return "", fmt.Errorf("artifact namespace is invalid")
	}
	fileName = path.Base(strings.ReplaceAll(strings.TrimSpace(fileName), "\\", "/"))
	if fileName == "" || fileName == "." || fileName == ".." {
		return "", fmt.Errorf("artifact file name is required")
	}
	return path.Join(prefix, businessID, namespaceID, fileName), nil
}
