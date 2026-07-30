package emaildelivery

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/google/uuid"
)

type deliveryRepositoryFake struct {
	claim         *ClaimedDelivery
	claimErr      error
	verifyErr     error
	markFailedErr error
	claimed       int
	verified      int
	sent          int
	failed        int
	messageID     string
}

func (r *deliveryRepositoryFake) Claim(_ context.Context, _ DeliveryMessage, _ string, _, _ time.Time) (*ClaimedDelivery, error) {
	r.claimed++
	return r.claim, r.claimErr
}
func (r *deliveryRepositoryFake) VerifyLease(context.Context, string, string, time.Time) error {
	r.verified++
	return r.verifyErr
}
func (r *deliveryRepositoryFake) MarkSent(_ context.Context, _, _, messageID, _, _ string, _ time.Time) error {
	r.sent++
	r.messageID = messageID
	return nil
}
func (r *deliveryRepositoryFake) MarkFailed(context.Context, string, string, string, time.Time) error {
	r.failed++
	return r.markFailedErr
}

type objectGetterFake struct {
	body []byte
	err  error
}

func (g *objectGetterFake) GetObject(
	_ context.Context,
	_ *s3.GetObjectInput,
	_ ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	if g.err != nil {
		return nil, g.err
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(g.body))}, nil
}

type rawEmailSenderFake struct {
	input *ses.SendRawEmailInput
	err   error
}

func (s *rawEmailSenderFake) SendRawEmail(
	_ context.Context,
	input *ses.SendRawEmailInput,
	_ ...func(*ses.Options),
) (*ses.SendRawEmailOutput, error) {
	s.input = input
	if s.err != nil {
		return nil, s.err
	}
	return &ses.SendRawEmailOutput{MessageId: aws.String("ses-message-1")}, nil
}

func TestWorkerSendsClaimedFinalPDFAndCommitsProviderMessageID(t *testing.T) {
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	message, claim := validDeliveryWork()
	repository := &deliveryRepositoryFake{claim: claim}
	objects := &objectGetterFake{body: []byte("%PDF-1.7\ninvoice")}
	email := &rawEmailSenderFake{}
	worker, err := NewWorker(WorkerOptions{
		Repository: repository, Objects: objects, Email: email,
		Bucket: "billeif-prod-invoices", SenderEmail: "billing@example.com",
		ConfigurationSet: "Billeif-prod-ses-events", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	err = worker.Process(context.Background(), message, "request-1:message-1")

	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if repository.claimed != 1 || repository.verified != 1 ||
		repository.sent != 1 || repository.failed != 0 ||
		repository.messageID != "ses-message-1" {
		t.Fatalf("repo claim/verify/sent/failed/message = %d/%d/%d/%d/%q",
			repository.claimed, repository.verified, repository.sent, repository.failed, repository.messageID)
	}
	if email.input == nil ||
		aws.ToString(email.input.Source) != "billing@example.com" ||
		len(email.input.Destinations) != 1 || email.input.Destinations[0] != claim.Recipient ||
		aws.ToString(email.input.ConfigurationSetName) != "Billeif-prod-ses-events" {
		t.Fatalf("SES input = %#v", email.input)
	}
	if len(email.input.Tags) != 2 ||
		aws.ToString(email.input.Tags[0].Name) != "delivery_id" ||
		aws.ToString(email.input.Tags[0].Value) != message.DeliveryID ||
		aws.ToString(email.input.Tags[1].Name) != "business_id" ||
		aws.ToString(email.input.Tags[1].Value) != message.BusinessID {
		t.Fatalf("SES tags = %#v", email.input.Tags)
	}
	raw := string(email.input.RawMessage.Data)
	for _, want := range []string{
		"From: Billeif <billing@example.com>",
		"To: buyer@example.com",
		"Subject: Invoice INV/26-27/000001",
		"Content-Type: application/pdf",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("raw message missing %q", want)
		}
	}
}

func TestWorkerVerifiesLeaseImmediatelyBeforeSES(t *testing.T) {
	message, claim := validDeliveryWork()
	repository := &deliveryRepositoryFake{claim: claim, verifyErr: ErrLeaseUnavailable}
	email := &rawEmailSenderFake{}
	worker, err := NewWorker(WorkerOptions{
		Repository: repository, Objects: &objectGetterFake{body: []byte("%PDF-1.7\ninvoice")},
		Email: email, Bucket: "bucket", SenderEmail: "billing@example.com",
		ConfigurationSet: "Billeif-prod-ses-events",
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	err = worker.Process(context.Background(), message, "owner")

	if !errors.Is(err, ErrLeaseUnavailable) {
		t.Fatalf("process error = %v, want lease unavailable", err)
	}
	if email.input != nil || repository.sent != 0 || repository.failed != 0 {
		t.Fatalf("SES/sent/failed = %#v/%d/%d", email.input, repository.sent, repository.failed)
	}
}

func TestWorkerInvalidPDFMarksFailedAndReturnsRetryableError(t *testing.T) {
	message, claim := validDeliveryWork()
	repository := &deliveryRepositoryFake{claim: claim}
	email := &rawEmailSenderFake{}
	worker, err := NewWorker(WorkerOptions{
		Repository: repository, Objects: &objectGetterFake{body: []byte("not a PDF")},
		Email: email, Bucket: "bucket", SenderEmail: "billing@example.com",
		ConfigurationSet: "Billeif-prod-ses-events",
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	err = worker.Process(context.Background(), message, "owner")

	if err == nil {
		t.Fatal("invalid PDF returned nil")
	}
	if repository.failed != 1 || email.input != nil {
		t.Fatalf("failed/SES = %d/%#v, want 1/nil", repository.failed, email.input)
	}
}

func TestWorkerTreatsTerminalDeliveryAsSuccessfulNoOp(t *testing.T) {
	message, _ := validDeliveryWork()
	repository := &deliveryRepositoryFake{claimErr: ErrDeliveryTerminal}
	worker, err := NewWorker(WorkerOptions{
		Repository: repository, Objects: &objectGetterFake{}, Email: &rawEmailSenderFake{},
		Bucket: "bucket", SenderEmail: "billing@example.com",
		ConfigurationSet: "Billeif-prod-ses-events",
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	if err := worker.Process(context.Background(), message, "owner"); err != nil {
		t.Fatalf("terminal no-op: %v", err)
	}
}

func validDeliveryWork() (DeliveryMessage, *ClaimedDelivery) {
	businessID := uuid.NewString()
	deliveryID := uuid.NewString()
	invoiceID := uuid.NewString()
	renderJobID := uuid.NewString()
	return DeliveryMessage{
			SchemaVersion: 1, Type: "send_invoice_pdf", BusinessID: businessID,
			DeliveryID: deliveryID, InvoiceID: invoiceID, RenderJobID: renderJobID,
		}, &ClaimedDelivery{
			DeliveryID: deliveryID, BusinessID: businessID, InvoiceID: invoiceID,
			RenderJobID: renderJobID, Recipient: "buyer@example.com",
			InvoiceNo: "INV/26-27/000001", InvoiceVersion: 3,
			ObjectKey:      "invoices/" + businessID + "/" + invoiceID + "/v3/final.pdf",
			OutputFilename: "INV_26-27_000001.pdf",
		}
}
