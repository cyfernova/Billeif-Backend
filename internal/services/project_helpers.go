package services

import (
	"strings"
)

func normalizeProjectID(projectID string) string {
	return strings.TrimSpace(projectID)
}

func projectIDPointer(projectID string) *string {
	projectID = normalizeProjectID(projectID)
	if projectID == "" {
		return nil
	}
	return &projectID
}

func extractProjectIDFromTags(tags map[string]interface{}) string {
	return normalizeProjectID(readStringCandidate(tags, "project_id"))
}

func mergeProjectIntoTags(tags map[string]interface{}, projectID string) map[string]interface{} {
	projectID = normalizeProjectID(projectID)
	merged := map[string]interface{}{}
	for key, value := range tags {
		merged[key] = value
	}
	if projectID == "" {
		delete(merged, "project_id")
		return merged
	}
	merged["project_id"] = projectID
	return merged
}

func syncProjectIDFromTags(projectID string, tags map[string]interface{}) string {
	projectID = normalizeProjectID(projectID)
	if projectID != "" {
		return projectID
	}
	return extractProjectIDFromTags(tags)
}
