package workers

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"invoice-backend/internal/config"
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

	w.wg.Add(2)
	go w.processInvoiceQueue(ctx)
	go w.processPaymentQueue(ctx)
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
	Type      string `json:"type"`
	InvoiceID string `json:"invoice_id"`
}

func (w *Worker) handleInvoiceMessage(ctx context.Context, body string) error {
	var msg InvoiceMessage
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		return err
	}

	w.log.Info("processing invoice message", "type", msg.Type, "invoice_id", msg.InvoiceID)

	switch msg.Type {
	case "generate_pdf":
		return w.generateInvoicePDF(ctx, msg.InvoiceID)
	default:
		w.log.Warn("unknown invoice message type", "type", msg.Type)
	}

	return nil
}

func (w *Worker) generateInvoicePDF(ctx context.Context, invoiceID string) error {
	invoice, err := w.svc.Invoice.GetForWorker(ctx, invoiceID)
	if err != nil {
		return err
	}

	pdfContent := w.buildInvoicePDFHTML(invoice)

	key := "invoices/" + invoiceID + "/invoice.pdf"
	if err := w.svc.S3.Upload(ctx, w.cfg.S3.BucketInvoices, key, []byte(pdfContent), "application/pdf"); err != nil {
		return err
	}

	pdfURL := w.svc.S3.GetObjectURL(w.cfg.S3.BucketInvoices, key)
	return w.svc.Invoice.UpdatePDFUrl(ctx, invoiceID, pdfURL)
}

func (w *Worker) buildInvoicePDFHTML(invoice *services.Invoice) string {
	return `<html><body><h1>Invoice</h1><p>Invoice generation placeholder</p></body></html>`
}

type PaymentMessage struct {
	Type      string `json:"type"`
	PaymentID string `json:"payment_id"`
}

func (w *Worker) handlePaymentMessage(ctx context.Context, body string) error {
	var msg PaymentMessage
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		return err
	}

	w.log.Info("processing payment message", "type", msg.Type, "payment_id", msg.PaymentID)
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
