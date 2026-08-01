package workers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sync"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
)

const canonicalRenderLeaseDuration = 2 * time.Minute

// NewInvoiceRenderLeaseOwner derives a bounded opaque token from one receive
// invocation and the SQS message identity.
func NewInvoiceRenderLeaseOwner(invocationID, messageID string) string {
	sum := sha256.Sum256([]byte(invocationID + "\x00" + messageID))
	return "invoice-render-" + hex.EncodeToString(sum[:])
}

func previewRenderAttemptObjectKey(canonicalKey, owner string) string {
	sum := sha256.Sum256([]byte(owner))
	return canonicalKey + ".attempt-" + hex.EncodeToString(sum[:])
}

type Worker struct {
	cfg    *config.Config
	svc    *services.Container
	sqs    *sqs.Client
	log    *logger.Logger
	stopCh chan struct{}
	wg     sync.WaitGroup
}

func New(cfg *config.Config, svc *services.Container, aws *awsclients.Config, log *logger.Logger) *Worker {
	return &Worker{
		cfg:    cfg,
		svc:    svc,
		sqs:    aws.SQS,
		log:    log,
		stopCh: make(chan struct{}),
	}
}

func (w *Worker) Start(ctx context.Context) {
	w.log.Info("starting background workers")

	w.wg.Add(2)
	go w.processInvoiceQueue(ctx)
	go w.processGSTQueue(ctx)
}

func (w *Worker) Stop() {
	w.log.Info("stopping background workers")
	close(w.stopCh)
	w.wg.Wait()
	w.log.Info("background workers stopped")
}

func (w *Worker) processInvoiceQueue(ctx context.Context) {
	defer w.wg.Done()
	w.processQueue(ctx, w.cfg.SQS.InvoiceQueue, w.handleInvoiceMessage)
}

func (w *Worker) processGSTQueue(ctx context.Context) {
	defer w.wg.Done()
	w.processQueue(ctx, w.cfg.SQS.GSTQueue, w.handleGSTMessage)
}

func (w *Worker) processQueue(ctx context.Context, queueURL string, handler func(context.Context, string, string) error) {
	log := w.log.Named("queue_worker").With("queue_url", queueURL)
	for {
		select {
		case <-w.stopCh:
			log.Info("queue worker stopped")
			return
		case <-ctx.Done():
			log.Info("queue worker context canceled")
			return
		default:
		}

		if queueURL == "" {
			log.Warn("queue URL not configured; sleeping")
			time.Sleep(5 * time.Second)
			continue
		}

		receiveStart := time.Now()
		result, err := w.sqs.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20,
			VisibilityTimeout:   30,
		})
		if err != nil {
			log.Error("failed to receive messages", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Debug("received messages", "count", len(result.Messages), "duration_ms", time.Since(receiveStart).Milliseconds())

		for _, msg := range result.Messages {
			if err := handler(ctx, *msg.Body, aws.ToString(msg.MessageId)); err != nil {
				log.Error("failed to process message", "error", err)
				continue
			}

			_, err := w.sqs.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(queueURL),
				ReceiptHandle: msg.ReceiptHandle,
			})
			if err != nil {
				log.Error("failed to delete message", "error", err)
				continue
			}
			log.Debug("message deleted from queue")
		}
	}
}

type InvoiceMessage struct {
	Type           string `json:"type"`
	InvoiceID      string `json:"invoice_id,omitempty"`
	DocumentID     string `json:"document_id,omitempty"`
	InvoiceVersion int    `json:"invoice_version,omitempty"`
	RenderJobID    string `json:"render_job_id,omitempty"`
}

func (w *Worker) handleInvoiceMessage(ctx context.Context, body, owner string) error {
	return ProcessInvoiceQueueMessageWithOwner(ctx, w.cfg, w.svc, w.log, body, NewInvoiceRenderLeaseOwner(uuid.NewString(), owner))
}

func (w *Worker) handleGSTMessage(ctx context.Context, body, _ string) error {
	return ProcessGSTQueueMessage(ctx, w.svc, w.log, body)
}

// ProcessInvoiceQueueMessage handles one invoice queue message in a transport-agnostic way.
func ProcessInvoiceQueueMessage(ctx context.Context, cfg *config.Config, svc *services.Container, log *logger.Logger, body string) error {
	return ProcessInvoiceQueueMessageWithOwner(ctx, cfg, svc, log, body, NewInvoiceRenderLeaseOwner(uuid.NewString(), ""))
}

// ProcessInvoiceQueueMessageWithOwner handles one invoice queue message with its unique transport owner.
func ProcessInvoiceQueueMessageWithOwner(ctx context.Context, cfg *config.Config, svc *services.Container, log *logger.Logger, body, owner string) error {
	if cfg == nil || svc == nil || log == nil {
		return fmt.Errorf("invalid dependencies for invoice queue processing")
	}

	var msg InvoiceMessage
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		return err
	}

	log.Info("processing invoice message", "type", msg.Type, "invoice_id", msg.InvoiceID, "document_id", msg.DocumentID, "render_job_id", msg.RenderJobID)

	switch msg.Type {
	case "generate_pdf":
		return generateInvoicePDF(ctx, cfg, svc, msg.InvoiceID)
	case "generate_document_pdf":
		return generateDocumentPDF(ctx, cfg, svc, log, msg.DocumentID, msg.RenderJobID, msg.InvoiceVersion, owner)
	default:
		log.Warn("unknown invoice message type", "type", msg.Type)
	}

	return nil
}

func generateInvoicePDF(ctx context.Context, cfg *config.Config, svc *services.Container, invoiceID string) error {
	invoice, err := svc.Invoice.GetForWorker(ctx, invoiceID)
	if err != nil {
		return err
	}

	document, err := svc.Document.GetForWorker(ctx, invoiceID)
	if err != nil || (document.DocumentType != models.DocumentTypeSalesInvoice && document.DocumentType != models.DocumentTypeBillOfSupply) {
		document = legacyInvoiceDocument(invoice)
	}

	profile, err := resolveRenderProfile(ctx, svc, document, "")
	if err != nil {
		return err
	}

	pdfContent, filename, err := renderDocumentPDF(ctx, svc, document, profile)
	if err != nil {
		return err
	}

	key := path.Join("invoices", invoiceID, filename)
	if err := svc.S3.Upload(ctx, cfg.S3.BucketInvoices, key, pdfContent, "application/pdf"); err != nil {
		return err
	}

	pdfURL := svc.S3.GetObjectURL(cfg.S3.BucketInvoices, key)
	return svc.Invoice.UpdatePDFUrl(ctx, invoiceID, pdfURL)
}

func generateDocumentPDF(
	ctx context.Context,
	cfg *config.Config,
	svc *services.Container,
	log *logger.Logger,
	documentID, renderJobID string,
	expectedInvoiceVersion int,
	owner string,
) error {
	document, err := svc.Document.GetForWorker(ctx, documentID)
	if err != nil {
		return err
	}

	job, err := svc.Document.GetRenderJobByBusiness(ctx, document.BusinessID, renderJobID)
	if err != nil {
		return fmt.Errorf("load exact render job: %w", err)
	}
	return processDocumentRenderJob(
		ctx,
		document,
		job,
		expectedInvoiceVersion,
		&servicePreviewRenderOperations{cfg: cfg, svc: svc, log: log, owner: owner},
	)
}

type PreviewRenderInProgressError struct {
	JobID string
}

func (e *PreviewRenderInProgressError) Error() string {
	return fmt.Sprintf("preview render job %q is already processing", e.JobID)
}

func (e *PreviewRenderInProgressError) Retryable() bool {
	return true
}

type FinalRenderInProgressError struct {
	JobID string
}

func (e *FinalRenderInProgressError) Error() string {
	return fmt.Sprintf("final render job %q is already processing", e.JobID)
}

func (e *FinalRenderInProgressError) Retryable() bool {
	return true
}

func processDocumentRenderJob(
	ctx context.Context,
	document *models.Document,
	job *models.DocumentRenderJob,
	expectedInvoiceVersion int,
	operations previewRenderOperations,
) error {
	if document == nil || job == nil {
		return errors.New("render job and document are required")
	}
	switch job.Kind {
	case models.RenderKindPreview:
		isGenericDocumentRender := expectedInvoiceVersion == 0 &&
			job.InvoiceID == nil &&
			job.SourceInvoiceVersion == nil &&
			job.ObjectKey == ""
		if isGenericDocumentRender {
			if operations == nil {
				return errors.New("generic document renderer is not configured")
			}
			return operations.renderGeneric(ctx, document, job)
		}
		if expectedInvoiceVersion < 1 ||
			job.SourceInvoiceVersion == nil ||
			*job.SourceInvoiceVersion != expectedInvoiceVersion {
			return errors.New("preview render message does not match exact versioned job")
		}
		return processPreviewRender(ctx, document, job, operations)
	case models.RenderKindFinal:
		return processFinalRender(ctx, document, job, expectedInvoiceVersion, operations)
	default:
		return fmt.Errorf("unsupported render job kind %q", job.Kind)
	}
}

type previewRenderOperations interface {
	canonicalRenderLeaseOwner() string
	currentInvoiceVersion(ctx context.Context, businessID, invoiceID string) (int, error)
	claimPreview(
		ctx context.Context,
		businessID, jobID string,
		sourceVersion int,
		owner string,
		now, leaseUntil time.Time,
	) (interfaces.PreviewRenderClaimState, error)
	loadProfile(ctx context.Context, businessID, profileID string) (*models.RenderProfile, error)
	render(ctx context.Context, document *models.Document, profile *models.RenderProfile) ([]byte, string, error)
	renderFinal(ctx context.Context, document *models.Document, profile *models.RenderProfile) ([]byte, string, error)
	upload(ctx context.Context, key string, content []byte) error
	uploadFinalIfAbsent(ctx context.Context, key string, content []byte) error
	verifyLease(ctx context.Context, businessID, jobID string, kind models.RenderKind, owner string, now time.Time) error
	markObsolete(ctx context.Context, businessID, jobID, owner string) error
	complete(
		ctx context.Context,
		businessID, jobID string,
		sourceVersion int,
		owner string,
		claimedObjectKey, selectedObjectKey, filename string,
	) (bool, error)
	fail(ctx context.Context, businessID, jobID, owner, message string) error
	renderGeneric(ctx context.Context, document *models.Document, job *models.DocumentRenderJob) error
	claimFinal(
		ctx context.Context,
		businessID, jobID string,
		sourceVersion int,
		owner string,
		now, leaseUntil time.Time,
	) (interfaces.FinalRenderClaimState, error)
	loadFinalSnapshot(
		ctx context.Context,
		businessID, invoiceID, jobID string,
		sourceVersion int,
	) (*models.Document, error)
	completeFinal(
		ctx context.Context,
		businessID, invoiceID, jobID string,
		sourceVersion int,
		owner string,
		objectKey, filename string,
	) (bool, error)
	failFinal(ctx context.Context, businessID, jobID, owner, message string) error
}

type servicePreviewRenderOperations struct {
	cfg   *config.Config
	svc   *services.Container
	log   *logger.Logger
	owner string
}

func (o *servicePreviewRenderOperations) canonicalRenderLeaseOwner() string { return o.owner }

func (o *servicePreviewRenderOperations) currentInvoiceVersion(
	ctx context.Context,
	businessID, invoiceID string,
) (int, error) {
	invoice, err := o.svc.Invoice.GetForWorker(ctx, invoiceID)
	if err != nil {
		return 0, err
	}
	if invoice.BusinessID != businessID {
		return 0, errors.New("preview invoice tenant mismatch")
	}
	return invoice.Version, nil
}

func (o *servicePreviewRenderOperations) claimPreview(
	ctx context.Context,
	businessID, jobID string,
	sourceVersion int,
	owner string,
	now, leaseUntil time.Time,
) (interfaces.PreviewRenderClaimState, error) {
	return o.svc.Document.ClaimPreviewRender(ctx, businessID, jobID, sourceVersion, owner, now, leaseUntil)
}

func (o *servicePreviewRenderOperations) loadProfile(
	ctx context.Context,
	businessID, profileID string,
) (*models.RenderProfile, error) {
	return o.svc.Document.GetRenderProfileByBusiness(ctx, businessID, profileID)
}

func (o *servicePreviewRenderOperations) render(
	ctx context.Context,
	document *models.Document,
	profile *models.RenderProfile,
) ([]byte, string, error) {
	return renderDocumentPDF(ctx, o.svc, document, profile)
}

func (o *servicePreviewRenderOperations) renderFinal(
	ctx context.Context,
	document *models.Document,
	profile *models.RenderProfile,
) ([]byte, string, error) {
	return renderFinalDocumentPDF(ctx, o.svc, document, profile)
}

func (o *servicePreviewRenderOperations) upload(
	ctx context.Context,
	key string,
	content []byte,
) error {
	return o.svc.S3.Upload(ctx, o.cfg.S3.BucketInvoices, key, content, "application/pdf")
}

func (o *servicePreviewRenderOperations) uploadFinalIfAbsent(
	ctx context.Context,
	key string,
	content []byte,
) error {
	return o.svc.S3.UploadIfAbsent(ctx, o.cfg.S3.BucketInvoices, key, content, "application/pdf")
}

func (o *servicePreviewRenderOperations) verifyLease(
	ctx context.Context,
	businessID, jobID string,
	kind models.RenderKind,
	owner string,
	now time.Time,
) error {
	return o.svc.Document.VerifyRenderLease(ctx, businessID, jobID, kind, owner, now)
}

func (o *servicePreviewRenderOperations) markObsolete(
	ctx context.Context,
	businessID, jobID, owner string,
) error {
	return o.svc.Document.ObsoletePreviewRender(ctx, businessID, jobID, owner)
}

func (o *servicePreviewRenderOperations) complete(
	ctx context.Context,
	businessID, jobID string,
	sourceVersion int,
	owner string,
	claimedObjectKey, selectedObjectKey, filename string,
) (bool, error) {
	return o.svc.Document.CompletePreviewRender(
		ctx,
		businessID,
		jobID,
		sourceVersion,
		owner,
		claimedObjectKey,
		selectedObjectKey,
		filename,
	)
}

func (o *servicePreviewRenderOperations) fail(
	ctx context.Context,
	businessID, jobID, owner, message string,
) error {
	return o.svc.Document.FailPreviewRender(ctx, businessID, jobID, owner, message)
}

func (o *servicePreviewRenderOperations) claimFinal(
	ctx context.Context,
	businessID, jobID string,
	sourceVersion int,
	owner string,
	now, leaseUntil time.Time,
) (interfaces.FinalRenderClaimState, error) {
	return o.svc.Document.ClaimFinalRender(ctx, businessID, jobID, sourceVersion, owner, now, leaseUntil)
}

func (o *servicePreviewRenderOperations) loadFinalSnapshot(
	ctx context.Context,
	businessID, invoiceID, jobID string,
	sourceVersion int,
) (*models.Document, error) {
	return o.svc.Document.LoadFinalRenderSnapshot(
		ctx,
		businessID,
		invoiceID,
		jobID,
		sourceVersion,
	)
}

func (o *servicePreviewRenderOperations) completeFinal(
	ctx context.Context,
	businessID, invoiceID, jobID string,
	sourceVersion int,
	owner string,
	objectKey, filename string,
) (bool, error) {
	return o.svc.Document.CompleteFinalRender(
		ctx,
		businessID,
		invoiceID,
		jobID,
		sourceVersion,
		owner,
		objectKey,
		filename,
	)
}

func (o *servicePreviewRenderOperations) failFinal(
	ctx context.Context,
	businessID, jobID, owner, message string,
) error {
	return o.svc.Document.FailFinalRender(ctx, businessID, jobID, owner, message)
}

func (o *servicePreviewRenderOperations) renderGeneric(
	ctx context.Context,
	document *models.Document,
	job *models.DocumentRenderJob,
) error {
	if err := o.svc.Document.MarkRenderJobProcessing(ctx, document.BusinessID, job.ID); err != nil {
		o.log.Warn(
			"failed to mark render job processing",
			"document_id",
			document.ID,
			"render_job_id",
			job.ID,
			"error",
			err,
		)
	}

	profile, err := resolveRenderProfile(ctx, o.svc, document, job.ID)
	if err != nil {
		_ = o.svc.Document.FailRenderJob(ctx, document.BusinessID, job.ID, err.Error())
		return err
	}

	pdfContent, filename, err := renderDocumentPDF(ctx, o.svc, document, profile)
	if err != nil {
		_ = o.svc.Document.FailRenderJob(ctx, document.BusinessID, job.ID, err.Error())
		return err
	}

	key := path.Join("documents", document.ID, filename)
	if err := o.svc.S3.Upload(
		ctx,
		o.cfg.S3.BucketInvoices,
		key,
		pdfContent,
		"application/pdf",
	); err != nil {
		_ = o.svc.Document.FailRenderJob(ctx, document.BusinessID, job.ID, err.Error())
		return err
	}

	pdfURL := o.svc.S3.GetObjectURL(o.cfg.S3.BucketInvoices, key)
	if err := o.svc.Document.UpdateRenderedPDF(
		ctx,
		document.ID,
		job.ID,
		pdfURL,
		filename,
	); err != nil {
		_ = o.svc.Document.FailRenderJob(ctx, document.BusinessID, job.ID, err.Error())
		return err
	}
	if document.DocumentType == models.DocumentTypeSalesInvoice {
		if err := o.svc.Invoice.UpdatePDFUrl(ctx, document.ID, pdfURL); err != nil {
			o.log.Warn(
				"failed to sync rendered sales invoice PDF to legacy invoice",
				"document_id",
				document.ID,
				"error",
				err,
			)
		}
	}
	return nil
}

func processFinalRender(
	ctx context.Context,
	document *models.Document,
	job *models.DocumentRenderJob,
	expectedInvoiceVersion int,
	operations previewRenderOperations,
) error {
	if document == nil || job == nil || operations == nil ||
		document.ID == "" || document.BusinessID == "" ||
		job.ID == "" || job.BusinessID != document.BusinessID ||
		job.Kind != models.RenderKindFinal ||
		job.DocumentID == nil || *job.DocumentID != document.ID ||
		job.InvoiceID == nil || *job.InvoiceID != document.ID ||
		job.SourceInvoiceVersion == nil ||
		*job.SourceInvoiceVersion < 1 ||
		expectedInvoiceVersion < 1 ||
		*job.SourceInvoiceVersion != expectedInvoiceVersion {
		return errors.New("final render job identity mismatch")
	}
	sourceVersion := *job.SourceInvoiceVersion
	owner := operations.canonicalRenderLeaseOwner()
	if owner == "" {
		return errors.New("canonical render lease owner is required")
	}
	if job.Status == models.RenderJobStatusCompleted ||
		job.Status == models.RenderJobStatusObsolete {
		return nil
	}
	expectedObjectKey := path.Join(
		"invoices",
		document.BusinessID,
		document.ID,
		fmt.Sprintf("v%d", sourceVersion),
		"final.pdf",
	)
	if job.ObjectKey != expectedObjectKey {
		return errors.New("final render object key mismatch")
	}

	claimNow := time.Now().UTC()
	claimState, err := operations.claimFinal(
		ctx,
		document.BusinessID,
		job.ID,
		sourceVersion,
		owner,
		claimNow,
		claimNow.Add(canonicalRenderLeaseDuration),
	)
	if err != nil {
		return fmt.Errorf("claim final render: %w", err)
	}
	switch claimState {
	case interfaces.FinalRenderClaimed:
	case interfaces.FinalRenderAlreadyProcessing:
		return &FinalRenderInProgressError{JobID: job.ID}
	case interfaces.FinalRenderAlreadyCompleted, interfaces.FinalRenderAlreadyObsolete:
		return nil
	default:
		return fmt.Errorf("claim final render returned unknown state %q", claimState)
	}

	currentVersion, err := operations.currentInvoiceVersion(
		ctx,
		document.BusinessID,
		document.ID,
	)
	if err != nil {
		_ = operations.failFinal(ctx, document.BusinessID, job.ID, owner, err.Error())
		return fmt.Errorf("load final invoice version: %w", err)
	}
	if currentVersion != sourceVersion {
		err := errors.New("final render invoice version changed")
		_ = operations.failFinal(ctx, document.BusinessID, job.ID, owner, err.Error())
		return err
	}

	frozen, err := operations.loadFinalSnapshot(
		ctx,
		document.BusinessID,
		document.ID,
		job.ID,
		sourceVersion,
	)
	if err != nil {
		_ = operations.failFinal(ctx, document.BusinessID, job.ID, owner, err.Error())
		return fmt.Errorf("load frozen final render snapshot: %w", err)
	}
	if err := validateFinalRenderSnapshot(frozen, document.BusinessID, document.ID); err != nil {
		_ = operations.failFinal(ctx, document.BusinessID, job.ID, owner, err.Error())
		return err
	}

	var profile *models.RenderProfile
	if job.RenderProfileID != nil {
		profile, err = operations.loadProfile(ctx, document.BusinessID, *job.RenderProfileID)
		if err != nil {
			_ = operations.failFinal(ctx, document.BusinessID, job.ID, owner, err.Error())
			return fmt.Errorf("load frozen final render profile: %w", err)
		}
	}
	content, filename, err := operations.renderFinal(ctx, frozen, profile)
	if err != nil {
		_ = operations.failFinal(ctx, document.BusinessID, job.ID, owner, err.Error())
		return fmt.Errorf("render private invoice final: %w", err)
	}
	if err := operations.verifyLease(ctx, document.BusinessID, job.ID, models.RenderKindFinal, owner, time.Now().UTC()); err != nil {
		return fmt.Errorf("verify final render lease before publish: %w", err)
	}
	if err := operations.uploadFinalIfAbsent(ctx, job.ObjectKey, content); err != nil {
		_ = operations.failFinal(ctx, document.BusinessID, job.ID, owner, err.Error())
		return fmt.Errorf("upload private invoice final: %w", err)
	}

	currentVersion, err = operations.currentInvoiceVersion(
		ctx,
		document.BusinessID,
		document.ID,
	)
	if err != nil {
		_ = operations.failFinal(ctx, document.BusinessID, job.ID, owner, err.Error())
		return fmt.Errorf("reload final invoice version: %w", err)
	}
	if currentVersion != sourceVersion {
		err := errors.New("final render invoice version changed before completion")
		_ = operations.failFinal(ctx, document.BusinessID, job.ID, owner, err.Error())
		return err
	}
	if _, err := operations.completeFinal(
		ctx,
		document.BusinessID,
		document.ID,
		job.ID,
		sourceVersion,
		owner,
		job.ObjectKey,
		filename,
	); err != nil {
		_ = operations.failFinal(ctx, document.BusinessID, job.ID, owner, err.Error())
		return fmt.Errorf("complete private invoice final: %w", err)
	}
	return nil
}

func validateFinalRenderSnapshot(
	document *models.Document,
	businessID, invoiceID string,
) error {
	if document == nil ||
		document.ID != invoiceID ||
		document.BusinessID != businessID ||
		document.Status != models.DocumentStatusIssued ||
		document.DraftState != models.DocumentDraftStateFinal ||
		document.SerialNumber == "" ||
		len(document.Lines) == 0 {
		return errors.New("final render snapshot identity mismatch")
	}
	var source struct {
		Seller models.PartySnapshot `json:"seller_snapshot"`
		Buyer  models.PartySnapshot `json:"buyer_snapshot"`
	}
	if err := json.Unmarshal([]byte(document.SourceLinkage), &source); err != nil ||
		source.Seller.IsEmpty() || source.Buyer.IsEmpty() {
		return errors.New("final render snapshot party identity mismatch")
	}
	return nil
}

func processPreviewRender(
	ctx context.Context,
	document *models.Document,
	job *models.DocumentRenderJob,
	operations previewRenderOperations,
) error {
	if document == nil || job == nil || operations == nil ||
		document.ID == "" || document.BusinessID == "" || job.ID == "" ||
		job.BusinessID != document.BusinessID ||
		job.Kind != models.RenderKindPreview ||
		job.DocumentID == nil || *job.DocumentID != document.ID ||
		job.InvoiceID == nil || *job.InvoiceID != document.ID ||
		job.SourceInvoiceVersion == nil || *job.SourceInvoiceVersion < 1 {
		return errors.New("preview render job identity mismatch")
	}
	sourceVersion := *job.SourceInvoiceVersion
	owner := operations.canonicalRenderLeaseOwner()
	if owner == "" {
		return errors.New("canonical render lease owner is required")
	}
	if job.Status == models.RenderJobStatusCompleted ||
		job.Status == models.RenderJobStatusObsolete {
		return nil
	}
	expectedObjectKey := path.Join(
		"invoices",
		document.BusinessID,
		document.ID,
		"previews",
		fmt.Sprintf("v%d", sourceVersion),
		job.ID+".pdf",
	)
	if job.ObjectKey != expectedObjectKey {
		return errors.New("preview render object key mismatch")
	}
	claimNow := time.Now().UTC()
	claimState, err := operations.claimPreview(
		ctx,
		document.BusinessID,
		job.ID,
		sourceVersion,
		owner,
		claimNow,
		claimNow.Add(canonicalRenderLeaseDuration),
	)
	if err != nil {
		return fmt.Errorf("claim preview render: %w", err)
	}
	switch claimState {
	case interfaces.PreviewRenderClaimed:
	case interfaces.PreviewRenderAlreadyProcessing:
		return &PreviewRenderInProgressError{JobID: job.ID}
	case interfaces.PreviewRenderAlreadyCompleted,
		interfaces.PreviewRenderAlreadyObsolete:
		return nil
	default:
		return fmt.Errorf("claim preview render returned unknown state %q", claimState)
	}

	currentVersion, err := operations.currentInvoiceVersion(
		ctx,
		document.BusinessID,
		document.ID,
	)
	if err != nil {
		_ = operations.fail(ctx, document.BusinessID, job.ID, owner, err.Error())
		return fmt.Errorf("load preview invoice version: %w", err)
	}
	if currentVersion != sourceVersion {
		return operations.markObsolete(ctx, document.BusinessID, job.ID, owner)
	}

	var profile *models.RenderProfile
	if job.RenderProfileID != nil {
		profile, err = operations.loadProfile(ctx, document.BusinessID, *job.RenderProfileID)
		if err != nil {
			_ = operations.fail(ctx, document.BusinessID, job.ID, owner, err.Error())
			return fmt.Errorf("load frozen preview render profile: %w", err)
		}
	}
	content, filename, err := operations.render(ctx, document, profile)
	if err != nil {
		_ = operations.fail(ctx, document.BusinessID, job.ID, owner, err.Error())
		return fmt.Errorf("render private invoice preview: %w", err)
	}
	if err := operations.verifyLease(ctx, document.BusinessID, job.ID, models.RenderKindPreview, owner, time.Now().UTC()); err != nil {
		return fmt.Errorf("verify preview render lease before publish: %w", err)
	}
	attemptObjectKey := previewRenderAttemptObjectKey(job.ObjectKey, owner)
	if err := operations.upload(ctx, attemptObjectKey, content); err != nil {
		_ = operations.fail(ctx, document.BusinessID, job.ID, owner, err.Error())
		return fmt.Errorf("upload private invoice preview: %w", err)
	}

	currentVersion, err = operations.currentInvoiceVersion(
		ctx,
		document.BusinessID,
		document.ID,
	)
	if err != nil {
		_ = operations.fail(ctx, document.BusinessID, job.ID, owner, err.Error())
		return fmt.Errorf("reload preview invoice version: %w", err)
	}
	if currentVersion != sourceVersion {
		return operations.markObsolete(ctx, document.BusinessID, job.ID, owner)
	}
	if _, err := operations.complete(
		ctx,
		document.BusinessID,
		job.ID,
		sourceVersion,
		owner,
		job.ObjectKey,
		attemptObjectKey,
		filename,
	); err != nil {
		_ = operations.fail(ctx, document.BusinessID, job.ID, owner, err.Error())
		return fmt.Errorf("complete private invoice preview: %w", err)
	}
	return nil
}

func resolveRenderProfile(ctx context.Context, svc *services.Container, document *models.Document, renderJobID string) (*models.RenderProfile, error) {
	if renderJobID != "" {
		job, err := svc.Document.GetRenderJobByBusiness(ctx, document.BusinessID, renderJobID)
		if err == nil && job.RenderProfileID != nil {
			return svc.Document.GetRenderProfileByBusiness(ctx, document.BusinessID, *job.RenderProfileID)
		}
	}
	if document.RenderProfileID != nil {
		profile, err := svc.Document.GetRenderProfileByBusiness(ctx, document.BusinessID, *document.RenderProfileID)
		if err == nil {
			return profile, nil
		}
	}
	profile, err := svc.Document.GetDefaultRenderProfileByBusiness(ctx, document.BusinessID)
	if err != nil {
		return nil, nil
	}
	return profile, nil
}

func legacyInvoiceDocument(invoice *services.Invoice) *models.Document {
	document := &models.Document{
		ID:                    invoice.ID,
		BusinessID:            invoice.BusinessID,
		DocumentType:          models.DocumentTypeSalesInvoice,
		PartyType:             models.DocumentPartyTypeCustomer,
		PartyID:               invoice.CustomerID,
		Status:                models.DocumentStatusIssued,
		DraftState:            models.DocumentDraftStateFinal,
		TaxMode:               models.DocumentTaxModeNonGST,
		GSTTreatment:          models.DocumentGSTTreatmentRegular,
		SerialNumber:          models.StringValue(invoice.InvoiceNo),
		IssueDate:             invoice.InvoiceDate,
		DueDate:               &invoice.DueDate,
		Currency:              invoice.Currency,
		Locale:                "en-IN",
		RenderProfileID:       invoice.RenderProfileID,
		Notes:                 invoice.Notes,
		Subtotal:              invoice.Subtotal,
		DiscountTotal:         invoice.Discount,
		TaxTotal:              invoice.Tax,
		Total:                 invoice.Total,
		PaidAmount:            invoice.PaidAmount,
		BalanceDue:            invoice.BalanceDue,
		ProfitSnapshotEnabled: true,
	}
	if invoice.Tax > 0 {
		document.TaxMode = models.DocumentTaxModeGST
	}
	for _, item := range invoice.Items {
		document.Lines = append(document.Lines, &models.DocumentLine{
			ID:                item.ID,
			DocumentID:        invoice.ID,
			ProductID:         item.ProductID,
			Description:       item.Description,
			Quantity:          item.Quantity,
			RemainingQuantity: item.Quantity,
			UnitPrice:         item.UnitPrice,
			DiscountAmount:    item.Discount,
			TaxRate:           item.TaxRate,
			TaxAmount:         item.Total - ((item.Quantity * item.UnitPrice) - item.Discount),
			LineSubtotal:      (item.Quantity * item.UnitPrice) - item.Discount,
			LineTotal:         item.Total,
			StockEffect:       "out",
		})
	}
	return document
}

type GSTQueueMessage struct {
	JobID string `json:"job_id"`
}

func ProcessGSTQueueMessage(ctx context.Context, svc *services.Container, log *logger.Logger, body string) error {
	if svc == nil || log == nil {
		return fmt.Errorf("invalid dependencies for gst queue processing")
	}

	var msg GSTQueueMessage
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		return err
	}
	if msg.JobID == "" {
		return fmt.Errorf("job_id is required")
	}

	log.Info("processing gst message", "job_id", msg.JobID)
	return svc.TaxCompliance.ProcessJobByID(ctx, msg.JobID)
}

func (w *Worker) SendToQueue(ctx context.Context, queueURL string, message interface{}) error {
	log := logger.FromContext(ctx).With("component", "worker", "operation", "send_to_queue", "queue_url", queueURL)
	body, err := json.Marshal(message)
	if err != nil {
		log.Error("failed to marshal queue message", "error", err)
		return err
	}

	_, err = w.sqs.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		log.Error("failed to send queue message", "error", err)
		return err
	}
	log.Debug("queue message sent", "body_size", len(body))
	return err
}

func (w *Worker) SendToQueueWithDelay(ctx context.Context, queueURL string, message interface{}, delaySeconds int32) error {
	log := logger.FromContext(ctx).With("component", "worker", "operation", "send_to_queue_with_delay", "queue_url", queueURL, "delay_seconds", delaySeconds)
	body, err := json.Marshal(message)
	if err != nil {
		log.Error("failed to marshal delayed queue message", "error", err)
		return err
	}

	_, err = w.sqs.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:     aws.String(queueURL),
		MessageBody:  aws.String(string(body)),
		DelaySeconds: delaySeconds,
	})
	if err != nil {
		log.Error("failed to send delayed queue message", "error", err)
		return err
	}
	log.Debug("delayed queue message sent", "body_size", len(body))
	return err
}
