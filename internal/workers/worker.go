package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sync"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

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

	w.wg.Add(3)
	go w.processInvoiceQueue(ctx)
	go w.processPaymentQueue(ctx)
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

func (w *Worker) processPaymentQueue(ctx context.Context) {
	defer w.wg.Done()
	w.processQueue(ctx, w.cfg.SQS.PaymentQueue, w.handlePaymentMessage)
}

func (w *Worker) processGSTQueue(ctx context.Context) {
	defer w.wg.Done()
	w.processQueue(ctx, w.cfg.SQS.GSTQueue, w.handleGSTMessage)
}

func (w *Worker) processQueue(ctx context.Context, queueURL string, handler func(context.Context, string) error) {
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
			if err := handler(ctx, *msg.Body); err != nil {
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
	Type        string `json:"type"`
	InvoiceID   string `json:"invoice_id,omitempty"`
	DocumentID  string `json:"document_id,omitempty"`
	RenderJobID string `json:"render_job_id,omitempty"`
}

func (w *Worker) handleInvoiceMessage(ctx context.Context, body string) error {
	return ProcessInvoiceQueueMessage(ctx, w.cfg, w.svc, w.log, body)
}

func (w *Worker) handleGSTMessage(ctx context.Context, body string) error {
	return ProcessGSTQueueMessage(ctx, w.svc, w.log, body)
}

// ProcessInvoiceQueueMessage handles one invoice queue message in a transport-agnostic way.
func ProcessInvoiceQueueMessage(ctx context.Context, cfg *config.Config, svc *services.Container, log *logger.Logger, body string) error {
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
		return generateDocumentPDF(ctx, cfg, svc, log, msg.DocumentID, msg.RenderJobID)
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

func generateDocumentPDF(ctx context.Context, cfg *config.Config, svc *services.Container, log *logger.Logger, documentID, renderJobID string) error {
	document, err := svc.Document.GetForWorker(ctx, documentID)
	if err != nil {
		return err
	}

	if err := svc.Document.MarkRenderJobProcessing(ctx, document.BusinessID, renderJobID); err != nil {
		log.Warn("failed to mark render job processing", "document_id", documentID, "render_job_id", renderJobID, "error", err)
	}

	profile, err := resolveRenderProfile(ctx, svc, document, renderJobID)
	if err != nil {
		_ = svc.Document.FailRenderJob(ctx, document.BusinessID, renderJobID, err.Error())
		return err
	}

	pdfContent, filename, err := renderDocumentPDF(ctx, svc, document, profile)
	if err != nil {
		_ = svc.Document.FailRenderJob(ctx, document.BusinessID, renderJobID, err.Error())
		return err
	}

	key := path.Join("documents", document.ID, filename)
	if err := svc.S3.Upload(ctx, cfg.S3.BucketInvoices, key, pdfContent, "application/pdf"); err != nil {
		_ = svc.Document.FailRenderJob(ctx, document.BusinessID, renderJobID, err.Error())
		return err
	}

	pdfURL := svc.S3.GetObjectURL(cfg.S3.BucketInvoices, key)
	if err := svc.Document.UpdateRenderedPDF(ctx, document.ID, renderJobID, pdfURL, filename); err != nil {
		_ = svc.Document.FailRenderJob(ctx, document.BusinessID, renderJobID, err.Error())
		return err
	}
	if document.DocumentType == models.DocumentTypeSalesInvoice {
		if err := svc.Invoice.UpdatePDFUrl(ctx, document.ID, pdfURL); err != nil {
			log.Warn("failed to sync rendered sales invoice PDF to legacy invoice", "document_id", document.ID, "error", err)
		}
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
		PartyID:               &invoice.CustomerID,
		Status:                models.DocumentStatusIssued,
		DraftState:            models.DocumentDraftStateFinal,
		TaxMode:               models.DocumentTaxModeNonGST,
		GSTTreatment:          models.DocumentGSTTreatmentRegular,
		SerialNumber:          invoice.InvoiceNo,
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

type PaymentMessage struct {
	Type      string `json:"type"`
	PaymentID string `json:"payment_id"`
}

func (w *Worker) handlePaymentMessage(ctx context.Context, body string) error {
	return ProcessPaymentQueueMessage(ctx, w.log, body)
}

// ProcessPaymentQueueMessage handles one payment queue message in a transport-agnostic way.
func ProcessPaymentQueueMessage(ctx context.Context, log *logger.Logger, body string) error {
	_ = ctx
	if log == nil {
		return fmt.Errorf("invalid dependencies for payment queue processing")
	}

	var msg PaymentMessage
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		return err
	}

	log.Info("processing payment message", "type", msg.Type, "payment_id", msg.PaymentID)
	return nil
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
