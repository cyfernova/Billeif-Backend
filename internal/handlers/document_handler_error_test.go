package handlers

import (
	"fmt"
	"net/http"
	"testing"

	"invoice-backend/internal/models"
)

func TestDocumentUpdateErrorStatusMapsWrappedDraftConflictToConflict(t *testing.T) {
	err := fmt.Errorf("update document: %w", &models.DocumentDraftConflictError{})

	if got := documentUpdateErrorStatus(err); got != http.StatusConflict {
		t.Fatalf("document update status = %d, want %d", got, http.StatusConflict)
	}
}
