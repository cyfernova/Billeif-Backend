package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

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

func (*queueMessageDocumentRepository) ClaimFinalRender(context.Context, string, string, int, string, time.Time, time.Time) (interfaces.FinalRenderClaimState, error) {
	return interfaces.FinalRenderClaimed, nil
}

func (r *queueMessageDocumentRepository) GetByIDInternal(context.Context, string) (*models.Document, error) {
	return r.document, nil
}

func (r *queueMessageDocumentRepository) GetRenderJob(context.Context, string, string) (*models.DocumentRenderJob, error) {
	return r.job, nil
}

func (r *queueMessageDocumentRepository) ClaimPreviewRender(
	_ context.Context,
	_, _ string,
	_ int,
	_ string,
	_ time.Time,
	_ time.Time,
) (interfaces.PreviewRenderClaimState, error) {
	return r.claimState, nil
}

func (*queueMessageDocumentRepository) ObsoletePreviewRender(context.Context, string, string, string) error {
	return nil
}

func (*queueMessageDocumentRepository) FailPreviewRender(context.Context, string, string, string, string) error {
	return nil
}

func (*queueMessageDocumentRepository) CompletePreviewRender(
	context.Context,
	string,
	string,
	int,
	string,
	string,
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
	owner               string
	claimOwner          string
	completeOwner       string
	finalCompleteKey    string
	finalConditionalPut bool
	verifyError         error
	verifyCalls         int
	invoiceVersions     []int
	versionCalls        int
	profileID           string
	rendered            bool
	processing          bool
	obsolete            bool
	claimState          interfaces.PreviewRenderClaimState
	uploadKey           string
	uploadError         error
	completeVersion     int
	completeKey         string
	completeName        string
	completed           bool
	genericError        error
	genericRendered     bool
	finalClaimState     interfaces.FinalRenderClaimState
	finalSnapshot       *models.Document
	finalCompleted      bool
	finalFailed         bool
	renderedDocument    *models.Document
	liveComplianceState string
	liveRenderCalls     int
	finalRenderCalls    int
	uploadedContent     []byte
}

func (f *fakePreviewRenderOperations) canonicalRenderLeaseOwner() string {
	if f.owner == "" {
		return "test-render-owner"
	}
	return f.owner
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
	_ context.Context,
	_, _ string,
	_ int,
	owner string,
	_ time.Time,
	_ time.Time,
) (interfaces.PreviewRenderClaimState, error) {
	f.claimOwner = owner
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
	f.liveRenderCalls++
	if f.liveComplianceState != "" {
		return []byte("live compliance: " + f.liveComplianceState), "invoice-preview.pdf", nil
	}
	return []byte("private preview"), "invoice-preview.pdf", nil
}

func (f *fakePreviewRenderOperations) renderFinal(
	_ context.Context,
	document *models.Document,
	_ *models.RenderProfile,
) ([]byte, string, error) {
	f.rendered = true
	f.renderedDocument = document
	f.finalRenderCalls++
	return []byte("frozen final: " + document.SerialNumber), "invoice-final.pdf", nil
}

func (f *fakePreviewRenderOperations) upload(_ context.Context, key string, content []byte) error {
	f.uploadKey = key
	f.uploadedContent = append([]byte(nil), content...)
	return f.uploadError
}

func (f *fakePreviewRenderOperations) uploadFinalIfAbsent(_ context.Context, key string, content []byte) error {
	f.finalConditionalPut = true
	f.uploadKey = key
	f.uploadedContent = append([]byte(nil), content...)
	return f.uploadError
}

func (f *fakePreviewRenderOperations) verifyLease(context.Context, string, string, models.RenderKind, string, time.Time) error {
	f.verifyCalls++
	return f.verifyError
}

func (f *fakePreviewRenderOperations) markObsolete(context.Context, string, string, string) error {
	f.obsolete = true
	return nil
}

func (f *fakePreviewRenderOperations) complete(
	_ context.Context,
	_, _ string,
	sourceVersion int,
	owner string,
	_ string,
	objectKey, filename string,
) (bool, error) {
	f.completeOwner = owner
	f.completeVersion = sourceVersion
	f.completeKey = objectKey
	f.completeName = filename
	f.completed = true
	return true, nil
}

func TestProcessPreviewRenderPropagatesExactLeaseOwner(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	owner := NewInvoiceRenderLeaseOwner("receive-123", "sqs-message-id-123")
	operations := &fakePreviewRenderOperations{
		owner:           owner,
		invoiceVersions: []int{version, version},
	}

	if err := processPreviewRender(context.Background(), document, job, operations); err != nil {
		t.Fatalf("process preview: %v", err)
	}
	if operations.claimOwner != operations.owner || operations.completeOwner != operations.owner {
		t.Fatalf("claim/complete owner = %q/%q, want %q", operations.claimOwner, operations.completeOwner, operations.owner)
	}
}

func (f *fakePreviewRenderOperations) fail(context.Context, string, string, string, string) error {
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
	string,
	time.Time,
	time.Time,
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
	_ context.Context,
	_, _, _ string,
	_ int,
	_ string,
	selectedObjectKey, _ string,
) (bool, error) {
	f.finalCompleteKey = selectedObjectKey
	f.finalCompleted = true
	return true, nil
}

func (f *fakePreviewRenderOperations) failFinal(context.Context, string, string, string, string) error {
	f.finalFailed = true
	return nil
}

func validFinalRenderSnapshot(document *models.Document) *models.Document {
	return &models.Document{
		ID:            document.ID,
		BusinessID:    document.BusinessID,
		Status:        models.DocumentStatusIssued,
		DraftState:    models.DocumentDraftStateFinal,
		SerialNumber:  "INV/26-27/000001",
		SourceLinkage: `{"seller_snapshot":{"name":"Frozen Seller"},"buyer_snapshot":{"name":"Frozen Buyer"}}`,
		Lines:         []*models.DocumentLine{{Description: "Frozen line"}},
	}
}

func TestCanonicalRenderPublishesOwnerSpecificImmutableAttempt(t *testing.T) {
	tests := []struct {
		name  string
		final bool
	}{
		{name: "preview"},
		{name: "final", final: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, job, version := validPreviewWorkerFixture()
			operations := &fakePreviewRenderOperations{owner: "receive-a", invoiceVersions: []int{version, version}}
			if test.final {
				job.Kind = models.RenderKindFinal
				job.ObjectKey = fmt.Sprintf("invoices/%s/%s/v%d/final.pdf", document.BusinessID, document.ID, version)
				operations.finalSnapshot = validFinalRenderSnapshot(document)
			}

			if err := processDocumentRenderJob(context.Background(), document, job, version, operations); err != nil {
				t.Fatalf("process canonical render: %v", err)
			}
			if test.final {
				if operations.uploadKey != job.ObjectKey || !operations.finalConditionalPut {
					t.Fatalf("final upload key/conditional = %q/%t, want deterministic conditional create at %q", operations.uploadKey, operations.finalConditionalPut, job.ObjectKey)
				}
			} else if operations.uploadKey == job.ObjectKey || operations.uploadKey == "" {
				t.Fatalf("preview attempt upload key = %q, must be immutable and distinct from %q", operations.uploadKey, job.ObjectKey)
			}
			selected := operations.completeKey
			if test.final {
				selected = operations.finalCompleteKey
			}
			if selected != operations.uploadKey {
				t.Fatalf("selected/upload key = %q/%q, want exact attempt key", selected, operations.uploadKey)
			}
		})
	}
}

func TestCanonicalRenderVerifiesLeaseImmediatelyBeforeUpload(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	operations := &fakePreviewRenderOperations{
		owner:           "receive-a",
		invoiceVersions: []int{version},
		verifyError:     errors.New("lease reclaimed by receive-b"),
	}

	err := processDocumentRenderJob(context.Background(), document, job, version, operations)
	if err == nil || operations.verifyCalls != 1 || operations.uploadKey != "" || operations.completed {
		t.Fatalf("stale publish result/error = %#v/%v, want fenced before upload", operations, err)
	}
}

func TestCanonicalRenderDoesNotCompleteWhenPublishFails(t *testing.T) {
	for _, final := range []bool{false, true} {
		t.Run(fmt.Sprintf("final=%t", final), func(t *testing.T) {
			document, job, version := validPreviewWorkerFixture()
			operations := &fakePreviewRenderOperations{
				owner:           "receive-a",
				invoiceVersions: []int{version},
				uploadError:     errors.New("publish failed"),
			}
			if final {
				job.Kind = models.RenderKindFinal
				job.ObjectKey = fmt.Sprintf("invoices/%s/%s/v%d/final.pdf", document.BusinessID, document.ID, version)
				operations.finalSnapshot = validFinalRenderSnapshot(document)
			}

			if err := processDocumentRenderJob(context.Background(), document, job, version, operations); err == nil {
				t.Fatal("publish failure returned success")
			}
			if operations.completed || operations.finalCompleted {
				t.Fatalf("publish failure marked render complete: %#v", operations)
			}
		})
	}
}

func TestFinalRenderChecksumMismatchDoesNotComplete(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	job.Kind = models.RenderKindFinal
	job.ObjectKey = fmt.Sprintf("invoices/%s/%s/v%d/final.pdf", document.BusinessID, document.ID, version)
	operations := &fakePreviewRenderOperations{
		owner:           "receive-after-profile-change",
		invoiceVersions: []int{version},
		finalSnapshot:   validFinalRenderSnapshot(document),
		uploadError:     services.ErrConditionalWriteContentMismatch,
	}

	err := processDocumentRenderJob(context.Background(), document, job, version, operations)
	if !errors.Is(err, services.ErrConditionalWriteContentMismatch) {
		t.Fatalf("checksum mismatch error = %v, want content mismatch", err)
	}
	if operations.finalCompleted || !operations.finalFailed {
		t.Fatalf("checksum mismatch completion/failure = %t/%t, want false/true", operations.finalCompleted, operations.finalFailed)
	}
}

func TestProcessDocumentRenderJobNoOpsObsoleteFinalDuplicate(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	job.Kind = models.RenderKindFinal
	job.ObjectKey = fmt.Sprintf("invoices/%s/%s/v%d/final.pdf", document.BusinessID, document.ID, version)
	operations := &fakePreviewRenderOperations{finalClaimState: interfaces.FinalRenderClaimState("obsolete")}

	if err := processDocumentRenderJob(context.Background(), document, job, version, operations); err != nil {
		t.Fatalf("obsolete final duplicate: %v", err)
	}
	if operations.rendered || operations.uploadKey != "" || operations.finalCompleted {
		t.Fatalf("obsolete final duplicate performed work: %#v", operations)
	}
}

func TestInvoiceRenderLeaseOwnerIsFreshPerReceive(t *testing.T) {
	first := NewInvoiceRenderLeaseOwner("receive-a", "message-123")
	second := NewInvoiceRenderLeaseOwner("receive-b", "message-123")
	if first == "" || second == "" || first == second {
		t.Fatalf("fresh receive owners = %q/%q, want distinct non-empty tokens", first, second)
	}
	if len(first) > 255 || len(second) > 255 {
		t.Fatalf("fresh receive owner exceeded schema bound: %d/%d", len(first), len(second))
	}
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
	if operations.uploadKey == objectKey || operations.completeKey != operations.uploadKey {
		t.Fatalf("keys upload/complete = %q/%q, want matching immutable attempt distinct from %q", operations.uploadKey, operations.completeKey, objectKey)
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
	if !operations.processing || operations.rendered || operations.uploadKey != "" || operations.completed {
		t.Fatalf("stale preview performed work: %#v", operations)
	}
}

func TestProcessPreviewRenderRechecksVersionBeforeCompletion(t *testing.T) {
	document, job, version := validPreviewWorkerFixture()
	operations := &fakePreviewRenderOperations{invoiceVersions: []int{version, version + 1}}

	if err := processPreviewRender(context.Background(), document, job, operations); err != nil {
		t.Fatalf("process concurrently changed preview: %v", err)
	}
	if !operations.rendered || operations.uploadKey == "" || operations.uploadKey == job.ObjectKey {
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
	if operations.versionCalls != 0 {
		t.Fatalf("version calls = %d, want zero before duplicate claim result", operations.versionCalls)
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
		nil,
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
		nil,
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

func TestProcessDocumentRenderJobFinalRetriesIgnoreLiveComplianceState(t *testing.T) {
	var rendered [][]byte
	for _, liveComplianceState := range []string{"IRN-A", "IRN-B"} {
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
			invoiceVersions:     []int{version, version},
			finalSnapshot:       frozen,
			liveComplianceState: liveComplianceState,
		}

		if err := processDocumentRenderJob(context.Background(), document, job, version, operations); err != nil {
			t.Fatalf("process final with live compliance %q: %v", liveComplianceState, err)
		}
		if operations.liveRenderCalls != 0 || operations.finalRenderCalls != 1 {
			t.Fatalf(
				"live/frozen render calls = %d/%d, want 0/1",
				operations.liveRenderCalls,
				operations.finalRenderCalls,
			)
		}
		rendered = append(rendered, operations.uploadedContent)
	}

	if !bytes.Equal(rendered[0], rendered[1]) {
		t.Fatalf("final retry bytes changed with live compliance: %q != %q", rendered[0], rendered[1])
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
	if !operations.rendered || !operations.completed ||
		operations.uploadKey == job.ObjectKey || operations.completeKey != operations.uploadKey {
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
