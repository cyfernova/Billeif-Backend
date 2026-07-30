package workers

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
)

type queueMessageDocumentRepository struct {
	interfaces.DocumentRepository
	document   *models.Document
	job        *models.DocumentRenderJob
	claimState interfaces.PreviewRenderClaimState
}

func (r *queueMessageDocumentRepository) GetByIDInternal(context.Context, string) (*models.Document, error) {
	return r.document, nil
}

func (r *queueMessageDocumentRepository) GetRenderJob(context.Context, string, string) (*models.DocumentRenderJob, error) {
	return r.job, nil
}

func (r *queueMessageDocumentRepository) ClaimPreviewRender(
	context.Context,
	string,
	string,
	int,
) (interfaces.PreviewRenderClaimState, error) {
	return r.claimState, nil
}

func (*queueMessageDocumentRepository) MarkPreviewRenderObsolete(context.Context, string, string) error {
	return nil
}

func (*queueMessageDocumentRepository) FailPreviewRender(context.Context, string, string, string) error {
	return nil
}

func (*queueMessageDocumentRepository) CompletePreviewRender(
	context.Context,
	string,
	string,
	int,
	string,
	string,
) (bool, error) {
	return true, nil
}

type queueMessageInvoiceRepository struct {
	interfaces.CanonicalInvoiceRepository
	invoice *models.Invoice
}

func (r *queueMessageInvoiceRepository) GetByIDInternal(context.Context, string) (*models.Invoice, error) {
	return r.invoice, nil
}

type fakePreviewRenderOperations struct {
	invoiceVersions []int
	versionCalls    int
	profileID       string
	rendered        bool
	processing      bool
	obsolete        bool
	claimState      interfaces.PreviewRenderClaimState
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

func (f *fakePreviewRenderOperations) claimPreview(
	context.Context,
	string,
	string,
	int,
) (interfaces.PreviewRenderClaimState, error) {
	f.processing = true
	if f.claimState != "" {
		return f.claimState, nil
	}
	return interfaces.PreviewRenderClaimed, nil
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

func TestProcessPreviewRenderReturnsRetryableErrorWhenJobAlreadyProcessing(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	operations := &fakePreviewRenderOperations{
		invoiceVersions: []int{version},
		claimState:      interfaces.PreviewRenderAlreadyProcessing,
	}

	err := processPreviewRender(context.Background(), document, job, operations)
	var retryable *PreviewRenderInProgressError
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("process duplicate error = %T %v, want typed retryable error", err, err)
	}
	if operations.rendered || operations.uploadKey != "" || operations.completed || operations.obsolete {
		t.Fatalf("processing duplicate performed work: %#v", operations)
	}
	if operations.versionCalls != 1 {
		t.Fatalf("version calls = %d, want one pre-claim check", operations.versionCalls)
	}
}

func TestProcessInvoiceQueueMessageReturnsRetryableErrorForProcessingPreview(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	cfg := &config.Config{}
	log := logger.NewWithEnv("test")
	awsCfg := &awsclients.Config{}
	documentService := services.NewDocumentService(
		nil,
		cfg,
		&queueMessageDocumentRepository{
			document:   document,
			job:        job,
			claimState: interfaces.PreviewRenderAlreadyProcessing,
		},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		awsCfg,
		log,
	)
	invoiceService := services.NewInvoiceService(
		nil,
		cfg,
		&queueMessageInvoiceRepository{invoice: &models.Invoice{
			ID:         document.ID,
			BusinessID: document.BusinessID,
			Version:    version,
		}},
		nil,
		nil,
		nil,
		documentService,
		awsCfg,
		nil,
		nil,
		log,
	)
	container := &services.Container{
		Document: documentService,
		Invoice:  invoiceService,
	}
	body := fmt.Sprintf(
		`{"type":"generate_document_pdf","document_id":%q,"render_job_id":%q,"invoice_version":%d}`,
		document.ID,
		job.ID,
		version,
	)

	err := ProcessInvoiceQueueMessage(context.Background(), cfg, container, log, body)

	var retryable *PreviewRenderInProgressError
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("queue processing error = %T %v, want typed retryable error", err, err)
	}
}

func TestProcessPreviewRenderTreatsCompletedAndObsoleteClaimsAsSuccessfulNoOps(t *testing.T) {
	tests := []struct {
		name       string
		claimState interfaces.PreviewRenderClaimState
	}{
		{name: "completed", claimState: interfaces.PreviewRenderAlreadyCompleted},
		{name: "obsolete", claimState: interfaces.PreviewRenderAlreadyObsolete},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, job, version := validPreviewWorkerFixture()
			operations := &fakePreviewRenderOperations{
				invoiceVersions: []int{version},
				claimState:      test.claimState,
			}

			if err := processPreviewRender(context.Background(), document, job, operations); err != nil {
				t.Fatalf("process terminal duplicate: %v", err)
			}
			if operations.rendered || operations.uploadKey != "" || operations.completed || operations.obsolete {
				t.Fatalf("terminal duplicate performed work: %#v", operations)
			}
		})
	}
}

func TestProcessDocumentRenderJobRejectsFinalKindWithTypedError(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	job.Kind = models.RenderKindFinal
	operations := &fakePreviewRenderOperations{}

	err := processDocumentRenderJob(
		context.Background(),
		document,
		job,
		version,
		operations,
	)
	var unsupported *UnsupportedFinalRenderError
	if !errors.As(err, &unsupported) {
		t.Fatalf("final render error = %T %v, want typed unsupported-final error", err, err)
	}
	if operations.versionCalls != 0 || operations.processing || operations.rendered {
		t.Fatalf("unsupported final render performed preview work: %#v", operations)
	}
}

func TestProcessDocumentRenderJobRejectsMalformedOrVersionMismatchedPreview(t *testing.T) {
	tests := []struct {
		name            string
		expectedVersion int
		mutate          func(*models.DocumentRenderJob)
	}{
		{name: "missing message version", expectedVersion: 0},
		{name: "mismatched message version", expectedVersion: 4},
		{
			name:            "missing job version",
			expectedVersion: 3,
			mutate: func(job *models.DocumentRenderJob) {
				job.SourceInvoiceVersion = nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, job, _ := validPreviewWorkerFixture()
			if test.mutate != nil {
				test.mutate(job)
			}
			operations := &fakePreviewRenderOperations{}

			if err := processDocumentRenderJob(
				context.Background(),
				document,
				job,
				test.expectedVersion,
				operations,
			); err == nil {
				t.Fatal("malformed preview must fail closed")
			}
			if operations.versionCalls != 0 || operations.processing || operations.rendered {
				t.Fatalf("malformed preview performed work: %#v", operations)
			}
		})
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
