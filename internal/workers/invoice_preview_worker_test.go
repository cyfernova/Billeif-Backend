package workers

import (
	"context"
	"errors"
	"testing"

	"invoice-backend/internal/models"

	"github.com/google/uuid"
)

type fakePreviewRenderOperations struct {
	invoiceVersions []int
	versionCalls    int
	profileID       string
	rendered        bool
	processing      bool
	obsolete        bool
	claimResult     *bool
	uploadKey       string
	completeVersion int
	completeKey     string
	completeName    string
	completed       bool
}

func (f *fakePreviewRenderOperations) currentInvoiceVersion(context.Context, string, string) (int, error) {
	if f.versionCalls >= len(f.invoiceVersions) {
		return 0, errors.New("unexpected invoice version lookup")
	}
	version := f.invoiceVersions[f.versionCalls]
	f.versionCalls++
	return version, nil
}

func (f *fakePreviewRenderOperations) claimPreview(context.Context, string, string, int) (bool, error) {
	f.processing = true
	if f.claimResult != nil {
		return *f.claimResult, nil
	}
	return true, nil
}

func (f *fakePreviewRenderOperations) loadProfile(_ context.Context, _ string, profileID string) (*models.RenderProfile, error) {
	f.profileID = profileID
	return &models.RenderProfile{ID: profileID}, nil
}

func (f *fakePreviewRenderOperations) render(
	context.Context,
	*models.Document,
	*models.RenderProfile,
) ([]byte, string, error) {
	f.rendered = true
	return []byte("private preview"), "invoice-preview.pdf", nil
}

func (f *fakePreviewRenderOperations) upload(_ context.Context, key string, _ []byte) error {
	f.uploadKey = key
	return nil
}

func (f *fakePreviewRenderOperations) markObsolete(context.Context, string, string) error {
	f.obsolete = true
	return nil
}

func (f *fakePreviewRenderOperations) complete(
	_ context.Context,
	_, _ string,
	sourceVersion int,
	objectKey, filename string,
) (bool, error) {
	f.completeVersion = sourceVersion
	f.completeKey = objectKey
	f.completeName = filename
	f.completed = true
	return true, nil
}

func (f *fakePreviewRenderOperations) fail(context.Context, string, string, string) error {
	return nil
}

func TestProcessPreviewRenderUsesFrozenProfileAndExactPrivateObjectKey(t *testing.T) {
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	jobID := uuid.NewString()
	profileID := uuid.NewString()
	version := 7
	objectKey := "invoices/" + businessID + "/" + invoiceID + "/previews/v7/" + jobID + ".pdf"
	document := &models.Document{ID: invoiceID, BusinessID: businessID}
	job := &models.DocumentRenderJob{
		ID:                   jobID,
		DocumentID:           models.StringPointer(invoiceID),
		InvoiceID:            models.StringPointer(invoiceID),
		BusinessID:           businessID,
		RenderProfileID:      models.StringPointer(profileID),
		Kind:                 models.RenderKindPreview,
		SourceInvoiceVersion: &version,
		ObjectKey:            objectKey,
		Status:               models.RenderJobStatusQueued,
	}
	operations := &fakePreviewRenderOperations{invoiceVersions: []int{version, version}}

	if err := processPreviewRender(context.Background(), document, job, operations); err != nil {
		t.Fatalf("process preview: %v", err)
	}
	if !operations.processing || !operations.rendered || !operations.completed {
		t.Fatalf("workflow processing/rendered/completed = %t/%t/%t", operations.processing, operations.rendered, operations.completed)
	}
	if operations.profileID != profileID {
		t.Fatalf("profile = %q, want frozen %q", operations.profileID, profileID)
	}
	if operations.uploadKey != objectKey || operations.completeKey != objectKey {
		t.Fatalf("keys upload/complete = %q/%q, want %q", operations.uploadKey, operations.completeKey, objectKey)
	}
	if operations.completeVersion != version || operations.completeName != "invoice-preview.pdf" {
		t.Fatalf("completion version/name = %d/%q", operations.completeVersion, operations.completeName)
	}
	if operations.obsolete {
		t.Fatal("current preview must not be marked obsolete")
	}
}

func TestProcessPreviewRenderMarksChangedInvoiceObsoleteBeforeRendering(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	operations := &fakePreviewRenderOperations{invoiceVersions: []int{version + 1}}

	if err := processPreviewRender(context.Background(), document, job, operations); err != nil {
		t.Fatalf("process stale preview: %v", err)
	}
	if !operations.obsolete {
		t.Fatal("stale preview must be marked obsolete")
	}
	if operations.processing || operations.rendered || operations.uploadKey != "" || operations.completed {
		t.Fatalf("stale preview performed work: %#v", operations)
	}
}

func TestProcessPreviewRenderRechecksVersionBeforeCompletion(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	operations := &fakePreviewRenderOperations{invoiceVersions: []int{version, version + 1}}

	if err := processPreviewRender(context.Background(), document, job, operations); err != nil {
		t.Fatalf("process concurrently changed preview: %v", err)
	}
	if !operations.rendered || operations.uploadKey != job.ObjectKey {
		t.Fatalf("render/upload = %t/%q", operations.rendered, operations.uploadKey)
	}
	if !operations.obsolete || operations.completed {
		t.Fatalf("obsolete/completed = %t/%t, want true/false", operations.obsolete, operations.completed)
	}
}

func TestProcessPreviewRenderNoOpsWhenDuplicateDidNotClaimJob(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	notClaimed := false
	operations := &fakePreviewRenderOperations{
		invoiceVersions: []int{version},
		claimResult:     &notClaimed,
	}

	if err := processPreviewRender(context.Background(), document, job, operations); err != nil {
		t.Fatalf("process duplicate preview: %v", err)
	}
	if operations.rendered || operations.uploadKey != "" || operations.completed || operations.obsolete {
		t.Fatalf("unclaimed duplicate performed work: %#v", operations)
	}
	if operations.versionCalls != 1 {
		t.Fatalf("version calls = %d, want one pre-claim check", operations.versionCalls)
	}
}

func TestProcessPreviewRenderRejectsMismatchedExactJob(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	job.InvoiceID = models.StringPointer(uuid.NewString())
	operations := &fakePreviewRenderOperations{invoiceVersions: []int{version}}

	if err := processPreviewRender(context.Background(), document, job, operations); err == nil {
		t.Fatal("mismatched preview job must fail closed")
	}
	if operations.versionCalls != 0 || operations.rendered || operations.uploadKey != "" {
		t.Fatalf("mismatched job performed work: %#v", operations)
	}
}

func validPreviewWorkerFixture() (*models.Document, *models.DocumentRenderJob, int) {
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	jobID := uuid.NewString()
	version := 3
	return &models.Document{ID: invoiceID, BusinessID: businessID}, &models.DocumentRenderJob{
		ID:                   jobID,
		DocumentID:           models.StringPointer(invoiceID),
		InvoiceID:            models.StringPointer(invoiceID),
		BusinessID:           businessID,
		Kind:                 models.RenderKindPreview,
		SourceInvoiceVersion: &version,
		ObjectKey:            "invoices/" + businessID + "/" + invoiceID + "/previews/v3/" + jobID + ".pdf",
		Status:               models.RenderJobStatusQueued,
	}, version
}
