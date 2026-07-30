package main

import (
	"context"
	"fmt"
	"testing"

	"invoice-backend/internal/emaildelivery"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/google/uuid"
)

type recordingDeliveryProcessor struct {
	messages []emaildelivery.DeliveryMessage
	owners   []string
	failID   string
}

func (p *recordingDeliveryProcessor) Process(
	_ context.Context,
	message emaildelivery.DeliveryMessage,
	owner string,
) error {
	p.messages = append(p.messages, message)
	p.owners = append(p.owners, owner)
	if message.DeliveryID == p.failID {
		return fmt.Errorf("retry")
	}
	return nil
}

func TestHandlerReturnsOnlyFailedSQSRecordIdentifiers(t *testing.T) {
	success := validSQSEmailDeliveryRecord("message-ok")
	failure := validSQSEmailDeliveryRecord("message-retry")
	processor := &recordingDeliveryProcessor{failID: deliveryIDFromRecord(t, failure)}
	handler := emailDeliveryHandler{worker: processor}
	ctx := lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{
		AwsRequestID: "billeif-request-1",
	})

	response, err := handler.Handle(ctx, events.SQSEvent{Records: []events.SQSMessage{
		success,
		failure,
		{MessageId: "malformed", Body: `{"type":"send_invoice_pdf","recipient":"secret@example.com"}`},
	}})

	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(response.BatchItemFailures) != 2 ||
		response.BatchItemFailures[0].ItemIdentifier != "message-retry" ||
		response.BatchItemFailures[1].ItemIdentifier != "malformed" {
		t.Fatalf("batch failures = %#v", response.BatchItemFailures)
	}
	if len(processor.owners) != 2 ||
		processor.owners[0] != "billeif-request-1:message-ok" ||
		processor.owners[1] != "billeif-request-1:message-retry" {
		t.Fatalf("lease owners = %#v", processor.owners)
	}
}

func TestDecodeDeliveryMessageRejectsUnknownAndTrailingJSON(t *testing.T) {
	for _, body := range []string{
		`{"schema_version":1,"type":"send_invoice_pdf","business_id":"x","delivery_id":"x","invoice_id":"x","render_job_id":"x","recipient":"secret@example.com"}`,
		`{"schema_version":1} {}`,
	} {
		if _, err := decodeDeliveryMessage(body); err == nil {
			t.Fatalf("invalid body accepted: %s", body)
		}
	}
}

func TestHandlerRejectsMissingSQSMessageIDForWholeInvocation(t *testing.T) {
	handler := emailDeliveryHandler{worker: &recordingDeliveryProcessor{}}
	ctx := lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{
		AwsRequestID: "billeif-request-1",
	})

	response, err := handler.Handle(ctx, events.SQSEvent{Records: []events.SQSMessage{
		{Body: validSQSEmailDeliveryRecord("ignored").Body},
	}})

	if err == nil {
		t.Fatal("missing MessageId returned nil error")
	}
	if len(response.BatchItemFailures) != 0 {
		t.Fatalf("missing MessageId emitted invalid partial failure: %#v", response)
	}
}

func validSQSEmailDeliveryRecord(messageID string) events.SQSMessage {
	return events.SQSMessage{
		MessageId: messageID,
		Body: fmt.Sprintf(
			`{"schema_version":1,"type":"send_invoice_pdf","business_id":%q,"delivery_id":%q,"invoice_id":%q,"render_job_id":%q}`,
			uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(),
		),
	}
}

func deliveryIDFromRecord(t *testing.T, record events.SQSMessage) string {
	t.Helper()
	message, err := decodeDeliveryMessage(record.Body)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return message.DeliveryID
}
