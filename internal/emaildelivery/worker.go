package emaildelivery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	sestypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
	"github.com/google/uuid"
)

const (
	deliveryMessageVersion = 1
	deliveryMessageType    = "send_invoice_pdf"
	deliveryLeaseDuration  = 2 * time.Minute
)

var (
	ErrDeliveryTerminal  = errors.New("email delivery is terminal")
	ErrLeaseUnavailable  = errors.New("email delivery lease is unavailable")
	ErrDeliveryMalformed = errors.New("email delivery work is malformed")
)

type DeliveryMessage struct {
	SchemaVersion int    `json:"schema_version"`
	Type          string `json:"type"`
	BusinessID    string `json:"business_id"`
	DeliveryID    string `json:"delivery_id"`
	InvoiceID     string `json:"invoice_id"`
	RenderJobID   string `json:"render_job_id"`
}

type ClaimedDelivery struct {
	DeliveryID     string
	BusinessID     string
	InvoiceID      string
	RenderJobID    string
	Recipient      string
	InvoiceNo      string
	InvoiceVersion int
	ObjectKey      string
	OutputFilename string
}

type Repository interface {
	Claim(
		ctx context.Context,
		message DeliveryMessage,
		owner string,
		now, leaseUntil time.Time,
	) (*ClaimedDelivery, error)
	VerifyLease(ctx context.Context, deliveryID, owner string, now time.Time) error
	MarkSent(
		ctx context.Context,
		deliveryID, owner, providerMessageID, source, subject string,
		now time.Time,
	) error
	MarkFailed(ctx context.Context, deliveryID, owner, failure string, now time.Time) error
}

type ObjectGetter interface {
	GetObject(
		ctx context.Context,
		input *s3.GetObjectInput,
		optFns ...func(*s3.Options),
	) (*s3.GetObjectOutput, error)
}

type RawEmailSender interface {
	SendRawEmail(
		ctx context.Context,
		input *ses.SendRawEmailInput,
		optFns ...func(*ses.Options),
	) (*ses.SendRawEmailOutput, error)
}

type WorkerOptions struct {
	Repository       Repository
	Objects          ObjectGetter
	Email            RawEmailSender
	Bucket           string
	SenderEmail      string
	ConfigurationSet string
	Now              func() time.Time
}

type Worker struct {
	repository       Repository
	objects          ObjectGetter
	email            RawEmailSender
	bucket           string
	senderEmail      string
	configurationSet string
	now              func() time.Time
}

func NewWorker(options WorkerOptions) (*Worker, error) {
	if options.Repository == nil || options.Objects == nil || options.Email == nil {
		return nil, errors.New("email delivery worker dependencies are required")
	}
	bucket := strings.TrimSpace(options.Bucket)
	sender := strings.TrimSpace(options.SenderEmail)
	configurationSet := strings.TrimSpace(options.ConfigurationSet)
	address, err := mail.ParseAddress(sender)
	if bucket == "" || err != nil || address.Address != sender || configurationSet == "" {
		return nil, errors.New("email delivery worker configuration is invalid")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Worker{
		repository: options.Repository, objects: options.Objects, email: options.Email,
		bucket: bucket, senderEmail: sender, configurationSet: configurationSet, now: now,
	}, nil
}

func (w *Worker) Process(ctx context.Context, message DeliveryMessage, owner string) error {
	if err := validateDeliveryMessage(message, owner); err != nil {
		return err
	}
	now := w.now().UTC()
	claim, err := w.repository.Claim(
		ctx, message, strings.TrimSpace(owner),
		now, now.Add(deliveryLeaseDuration),
	)
	if errors.Is(err, ErrDeliveryTerminal) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := validateClaim(message, claim); err != nil {
		return w.failClaim(ctx, message.DeliveryID, owner, err)
	}

	pdf, err := w.downloadPDF(ctx, claim.ObjectKey)
	if err != nil {
		return w.failClaim(ctx, message.DeliveryID, owner, err)
	}
	subject := "Invoice " + strings.TrimSpace(claim.InvoiceNo)
	filename := strings.TrimSpace(claim.OutputFilename)
	if filename == "" {
		filename = claim.InvoiceNo
	}
	raw, err := BuildRawMessage(RawMessage{
		Source:    fmt.Sprintf("Billeif <%s>", w.senderEmail),
		Recipient: claim.Recipient,
		Subject:   subject,
		TextBody:  "Your invoice is attached.",
		Filename:  filename,
		PDF:       pdf,
	})
	if err != nil {
		return w.failClaim(ctx, message.DeliveryID, owner, err)
	}

	if err := w.repository.VerifyLease(ctx, message.DeliveryID, owner, w.now().UTC()); err != nil {
		return err
	}
	// SES SendRawEmail has no idempotency token. The live lease closes normal
	// SQS races, but a process exit after SES accepts and before MarkSent can resend.
	result, err := w.email.SendRawEmail(ctx, &ses.SendRawEmailInput{
		Source:               aws.String(w.senderEmail),
		Destinations:         []string{claim.Recipient},
		ConfigurationSetName: aws.String(w.configurationSet),
		RawMessage:           &sestypes.RawMessage{Data: raw},
		Tags: []sestypes.MessageTag{
			{Name: aws.String("delivery_id"), Value: aws.String(message.DeliveryID)},
			{Name: aws.String("business_id"), Value: aws.String(message.BusinessID)},
		},
	})
	if err != nil {
		return w.failClaim(ctx, message.DeliveryID, owner, fmt.Errorf("send raw email: %w", err))
	}
	if result == nil {
		return w.failClaim(ctx, message.DeliveryID, owner, errors.New("SES returned no response"))
	}
	messageID := strings.TrimSpace(aws.ToString(result.MessageId))
	if messageID == "" {
		return w.failClaim(ctx, message.DeliveryID, owner, errors.New("SES returned no message ID"))
	}
	if err := w.repository.MarkSent(
		ctx, message.DeliveryID, owner, messageID, w.senderEmail, subject, w.now().UTC(),
	); err != nil {
		return fmt.Errorf("commit sent delivery: %w", err)
	}
	return nil
}

func (w *Worker) downloadPDF(ctx context.Context, objectKey string) ([]byte, error) {
	result, err := w.objects.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return nil, fmt.Errorf("download final PDF: %w", err)
	}
	if result == nil || result.Body == nil {
		return nil, errors.New("download final PDF: empty object response")
	}
	defer result.Body.Close()
	data, err := io.ReadAll(io.LimitReader(result.Body, maxPDFBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read final PDF: %w", err)
	}
	if len(data) > maxPDFBytes {
		return nil, errors.New("final PDF exceeds size limit")
	}
	if len(data) < len("%PDF-") || string(data[:len("%PDF-")]) != "%PDF-" {
		return nil, errors.New("final object is not a PDF")
	}
	return data, nil
}

func (w *Worker) failClaim(
	ctx context.Context,
	deliveryID, owner string,
	failure error,
) error {
	message := strings.TrimSpace(failure.Error())
	if len(message) > 1000 {
		message = message[:1000]
	}
	if err := w.repository.MarkFailed(
		ctx, deliveryID, owner, message, w.now().UTC(),
	); err != nil {
		return errors.Join(failure, fmt.Errorf("commit failed delivery: %w", err))
	}
	return failure
}

func validateDeliveryMessage(message DeliveryMessage, owner string) error {
	if message.SchemaVersion != deliveryMessageVersion ||
		message.Type != deliveryMessageType ||
		!validDeliveryUUID(message.BusinessID) ||
		!validDeliveryUUID(message.DeliveryID) ||
		!validDeliveryUUID(message.InvoiceID) ||
		!validDeliveryUUID(message.RenderJobID) ||
		strings.TrimSpace(owner) == "" {
		return ErrDeliveryMalformed
	}
	return nil
}

func validateClaim(message DeliveryMessage, claim *ClaimedDelivery) error {
	if claim == nil ||
		claim.DeliveryID != message.DeliveryID ||
		claim.BusinessID != message.BusinessID ||
		claim.InvoiceID != message.InvoiceID ||
		claim.RenderJobID != message.RenderJobID ||
		claim.InvoiceVersion < 1 ||
		strings.TrimSpace(claim.InvoiceNo) == "" ||
		strings.TrimSpace(claim.Recipient) == "" ||
		claim.ObjectKey != fmt.Sprintf(
			"invoices/%s/%s/v%d/final.pdf",
			message.BusinessID, message.InvoiceID, claim.InvoiceVersion,
		) {
		return ErrDeliveryMalformed
	}
	return nil
}

func validDeliveryUUID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}
