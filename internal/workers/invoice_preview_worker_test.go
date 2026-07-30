package workers

import (
	"context"
	"encoding/json"
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
	invoiceVersions  []int
	versionCalls     int
	profileID        string
	rendered         bool
	processing       bool
	obsolete         bool
	claimState       interfaces.PreviewRenderClaimState
	uploadKey        string
	completeVersion  int
	completeKey      string
	completeName     string
	completed        bool
	genericError     error
	genericRendered  bool
	finalClaimState  interfaces.FinalRenderClaimState
	finalSnapshot    *models.Document
	finalCompleted   bool
	finalFailed      bool
	renderedDocument *models.Document
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
	_ context.Context,
	document *models.Document,
	_ *models.RenderProfile,
) ([]byte, string, error) {
	f.rendered = true
	f.renderedDocument = document
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

func (f *fakePreviewRenderOperations) renderGeneric(
	context.Context,
	*models.Document,
	*models.DocumentRenderJob,
) error {
	f.genericRendered = true
	return f.genericError
}

func (f *fakePreviewRenderOperations) claimFinal(
	context.Context,
	string,
	string,
	int,
) (interfaces.FinalRenderClaimState, error) {
	if f.finalClaimState != "" {
		return f.finalClaimState, nil
	}
	return interfaces.FinalRenderClaimed, nil
}

func (f *fakePreviewRenderOperations) loadFinalSnapshot(
	context.Context,
	string,
	string,
	string,
	int,
) (*models.Document, error) {
	return f.finalSnapshot, nil
}

func (f *fakePreviewRenderOperations) completeFinal(
	context.Context,
	string,
	string,
	string,
	int,
	string,
	string,
) (bool, error) {
	f.finalCompleted = true
	return true, nil
}

func (f *fakePreviewRenderOperations) failFinal(context.Context, string, string, string) error {
	f.finalFailed = true
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

func TestProcessDocumentRenderJobRejectsMalformedFinalIdentity(t *testing.T) {
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
	if err == nil {
		t.Fatal("malformed final render identity succeeded")
	}
	if operations.versionCalls != 0 || operations.processing || operations.rendered {
		t.Fatalf("malformed final render performed work: %#v", operations)
	}
}

func TestProcessDocumentRenderJobRendersCanonicalFinalFromFrozenPrivateSnapshot(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	job.Kind = models.RenderKindFinal
	job.ObjectKey = fmt.Sprintf(
		"invoices/%s/%s/v%d/final.pdf",
		document.BusinessID,
		document.ID,
		version,
	)
	profileID := uuid.NewString()
	job.RenderProfileID = &profileID
	frozen := &models.Document{
		ID:            document.ID,
		BusinessID:    document.BusinessID,
		Status:        models.DocumentStatusIssued,
		DraftState:    models.DocumentDraftStateFinal,
		SerialNumber:  "INV/26-27/000001",
		SourceLinkage: `{"seller_snapshot":{"name":"Frozen Seller"},"buyer_snapshot":{"name":"Frozen Buyer"}}`,
		Lines: []*models.DocumentLine{{
			ID:          uuid.NewString(),
			DocumentID:  document.ID,
			Description: "Frozen issued line",
		}},
	}
	operations := &fakePreviewRenderOperations{
		invoiceVersions: []int{version, version},
		finalSnapshot:   frozen,
	}

	err := processDocumentRenderJob(context.Background(), document, job, version, operations)

	if err != nil {
		t.Fatalf("process canonical final: %v", err)
	}
	if operations.renderedDocument != frozen || operations.uploadKey != job.ObjectKey ||
		!operations.finalCompleted || operations.finalFailed {
		t.Fatalf("final render workflow state: %#v", operations)
	}
	if operations.profileID != profileID {
		t.Fatalf("final profile = %q, want frozen %q", operations.profileID, profileID)
	}
}

func TestProcessDocumentRenderJobReturnsRetryableErrorForProcessingFinalDuplicate(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	job.Kind = models.RenderKindFinal
	job.ObjectKey = fmt.Sprintf(
		"invoices/%s/%s/v%d/final.pdf",
		document.BusinessID,
		document.ID,
		version,
	)
	operations := &fakePreviewRenderOperations{
		invoiceVersions: []int{version},
		finalClaimState: interfaces.FinalRenderAlreadyProcessing,
	}

	err := processDocumentRenderJob(context.Background(), document, job, version, operations)

	var retryable *FinalRenderInProgressError
	if !errors.As(err, &retryable) || !retryable.Retryable() {
		t.Fatalf("processing final error = %T %v, want typed retryable", err, err)
	}
	if operations.rendered || operations.finalCompleted {
		t.Fatalf("processing duplicate performed work: %#v", operations)
	}
}

func TestProcessDocumentRenderJobNoOpsCompletedFinalDuplicate(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	job.Kind = models.RenderKindFinal
	job.ObjectKey = fmt.Sprintf(
		"invoices/%s/%s/v%d/final.pdf",
		document.BusinessID,
		document.ID,
		version,
	)
	operations := &fakePreviewRenderOperations{
		invoiceVersions: []int{version},
		finalClaimState: interfaces.FinalRenderAlreadyCompleted,
	}

	if err := processDocumentRenderJob(context.Background(), document, job, version, operations); err != nil {
		t.Fatalf("completed final duplicate: %v", err)
	}
	if operations.rendered || operations.finalCompleted {
		t.Fatalf("completed duplicate performed work: %#v", operations)
	}
}

func TestProcessDocumentRenderJobFailsFinalOnVersionDriftBeforeAndAfterRender(t *testing.T) {
	tests := []struct {
		name     string
		versions []int
		rendered bool
	}{
		{name: "before render", versions: []int{4}, rendered: false},
		{name: "before completion", versions: []int{3, 4}, rendered: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, job, version := validPreviewWorkerFixture()
			job.Kind = models.RenderKindFinal
			job.ObjectKey = fmt.Sprintf(
				"invoices/%s/%s/v%d/final.pdf",
				document.BusinessID,
				document.ID,
				version,
			)
			frozen := &models.Document{
				ID:            document.ID,
				BusinessID:    document.BusinessID,
				Status:        models.DocumentStatusIssued,
				DraftState:    models.DocumentDraftStateFinal,
				SerialNumber:  "INV/26-27/000001",
				SourceLinkage: `{"seller_snapshot":{"name":"Frozen Seller"},"buyer_snapshot":{"name":"Frozen Buyer"}}`,
				Lines:         []*models.DocumentLine{{Description: "Frozen line"}},
			}
			operations := &fakePreviewRenderOperations{
				invoiceVersions: test.versions,
				finalSnapshot:   frozen,
			}

			err := processDocumentRenderJob(context.Background(), document, job, version, operations)

			if err == nil || !operations.finalFailed || operations.finalCompleted {
				t.Fatalf("version drift result/error = %#v/%v", operations, err)
			}
			if operations.rendered != test.rendered {
				t.Fatalf("rendered = %v, want %v", operations.rendered, test.rendered)
			}
		})
	}
}

func TestProcessDocumentRenderJobRoutesCanonicalPreviewToPrivateRenderer(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	legacyRendererReached := errors.New("legacy generic renderer reached")
	operations := &fakePreviewRenderOperations{
		invoiceVersions: []int{version, version},
		genericError:    legacyRendererReached,
	}

	if err := processDocumentRenderJob(
		context.Background(),
		document,
		job,
		version,
		operations,
	); err != nil {
		t.Fatalf("process canonical preview: %v", err)
	}
	if operations.genericRendered {
		t.Fatal("canonical preview entered legacy generic renderer")
	}
	if !operations.rendered || !operations.completed || operations.uploadKey != job.ObjectKey {
		t.Fatalf("canonical preview workflow state: %#v", operations)
	}
}

func TestProcessDocumentRenderJobRoutesRequestRenderGenericShapeToLegacyRenderer(t *testing.T) {
	businessID := uuid.NewString()
	documentID := uuid.NewString()
	jobID := uuid.NewString()
	document := &models.Document{ID: documentID, BusinessID: businessID}
	job := &models.DocumentRenderJob{
		ID:         jobID,
		DocumentID: models.StringPointer(documentID),
		BusinessID: businessID,
		Kind:       models.RenderKindPreview,
		Status:     models.RenderJobStatusQueued,
	}
	var envelope InvoiceMessage
	body := fmt.Sprintf(
		`{"type":"generate_document_pdf","document_id":%q,"render_job_id":%q}`,
		documentID,
		jobID,
	)
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("decode RequestRenderByBusiness-shaped envelope: %v", err)
	}
	legacyRendererReached := errors.New("legacy generic renderer reached")
	operations := &fakePreviewRenderOperations{genericError: legacyRendererReached}

	err := processDocumentRenderJob(
		context.Background(),
		document,
		job,
		envelope.InvoiceVersion,
		operations,
	)

	if !errors.Is(err, legacyRendererReached) || !operations.genericRendered {
		t.Fatalf("generic render result = %T %v, want legacy renderer sentinel", err, err)
	}
	if operations.versionCalls != 0 || operations.processing || operations.rendered {
		t.Fatalf("generic job entered canonical preview path: %#v", operations)
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
		{
			name:            "partial canonical metadata without message version",
			expectedVersion: 0,
			mutate: func(job *models.DocumentRenderJob) {
				job.InvoiceID = nil
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
