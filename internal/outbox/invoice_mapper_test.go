package outbox

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"invoice-backend/internal/models"

	"github.com/google/uuid"
)

func TestMapInvoiceEventToSQSMessageMapsPreviewEnvelopeExactly(t *testing.T) {
	event, invoiceID, renderJobID := validPreviewOutboxEvent()

	message, err := MapInvoiceEventToSQSMessage(event)

	if err != nil {
		t.Fatalf("map preview event: %v", err)
	}
	want := fmt.Sprintf(
		`{"type":"generate_document_pdf","invoice_id":%q,"document_id":%q,"invoice_version":7,"render_job_id":%q}`,
		invoiceID,
		invoiceID,
		renderJobID,
	)
	if string(message) != want {
		t.Fatalf("mapped preview = %s, want %s", message, want)
	}
}

func TestMapInvoiceEventToSQSMessageMapsIssuedEventExactly(t *testing.T) {
	event, invoiceID, renderJobID := validIssuedOutboxEvent()

	message, err := MapInvoiceEventToSQSMessage(event)

	if err != nil {
		t.Fatalf("map issued event: %v", err)
	}
	want := fmt.Sprintf(
		`{"type":"generate_document_pdf","invoice_id":%q,"document_id":%q,"invoice_version":8,"render_job_id":%q}`,
		invoiceID,
		invoiceID,
		renderJobID,
	)
	if string(message) != want {
		t.Fatalf("mapped issued = %s, want %s", message, want)
	}
}

func TestMapInvoiceEventToSQSMessageRejectsInvalidStoredEvents(t *testing.T) {
	validPreview, _, _ := validPreviewOutboxEvent()
	validIssued, _, _ := validIssuedOutboxEvent()
	tests := []struct {
		name   string
		mutate func(*models.OutboxEvent)
		base   *models.OutboxEvent
	}{
		{
			name: "malformed JSON",
			base: cloneOutboxEvent(validPreview),
			mutate: func(event *models.OutboxEvent) {
				event.Payload = `{"schema_version":`
			},
		},
		{
			name: "unsupported event type",
			base: cloneOutboxEvent(validPreview),
			mutate: func(event *models.OutboxEvent) {
				event.EventType = "invoice.deleted.v1"
			},
		},
		{
			name: "unsupported schema version",
			base: cloneOutboxEvent(validPreview),
			mutate: func(event *models.OutboxEvent) {
				event.Payload = strings.Replace(event.Payload, `"schema_version":1`, `"schema_version":2`, 1)
			},
		},
		{
			name: "invalid business ID",
			base: cloneOutboxEvent(validPreview),
			mutate: func(event *models.OutboxEvent) {
				event.BusinessID = "not-a-uuid"
			},
		},
		{
			name: "invalid aggregate type",
			base: cloneOutboxEvent(validPreview),
			mutate: func(event *models.OutboxEvent) {
				event.AggregateType = "document"
			},
		},
		{
			name: "aggregate payload mismatch",
			base: cloneOutboxEvent(validPreview),
			mutate: func(event *models.OutboxEvent) {
				event.AggregateID = uuid.NewString()
			},
		},
		{
			name: "missing event ID",
			base: cloneOutboxEvent(validPreview),
			mutate: func(event *models.OutboxEvent) {
				event.ID = ""
			},
		},
		{
			name: "missing issued render job ID",
			base: cloneOutboxEvent(validIssued),
			mutate: func(event *models.OutboxEvent) {
				event.Payload = fmt.Sprintf(
					`{"schema_version":1,"aggregate_type":"invoice","aggregate_id":%q,"invoice_no":"INV/1","invoice_version":8}`,
					event.AggregateID,
				)
			},
		},
		{
			name: "missing preview document ID",
			base: cloneOutboxEvent(validPreview),
			mutate: func(event *models.OutboxEvent) {
				event.Payload = strings.Replace(
					event.Payload,
					`"document_id":"`+event.AggregateID+`"`,
					`"document_id":""`,
					1,
				)
			},
		},
		{
			name: "non-positive preview version",
			base: cloneOutboxEvent(validPreview),
			mutate: func(event *models.OutboxEvent) {
				event.Payload = strings.Replace(event.Payload, `"invoice_version":7`, `"invoice_version":0`, 1)
			},
		},
		{
			name: "non-positive issued version",
			base: cloneOutboxEvent(validIssued),
			mutate: func(event *models.OutboxEvent) {
				event.Payload = strings.Replace(event.Payload, `"invoice_version":8`, `"invoice_version":0`, 1)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := cloneOutboxEvent(test.base)
			test.mutate(event)

			_, err := MapInvoiceEventToSQSMessage(event)

			var mappingError *InvoiceEventMappingError
			if !errors.As(err, &mappingError) {
				t.Fatalf("mapping error = %T %v, want typed sanitized error", err, err)
			}
			if strings.Contains(err.Error(), event.Payload) {
				t.Fatalf("mapping error leaked payload: %v", err)
			}
		})
	}
}

func validPreviewOutboxEvent() (*models.OutboxEvent, string, string) {
	invoiceID := uuid.NewString()
	renderJobID := uuid.NewString()
	return &models.OutboxEvent{
		ID:            uuid.NewString(),
		BusinessID:    uuid.NewString(),
		AggregateType: "invoice",
		AggregateID:   invoiceID,
		EventType:     "invoice.preview.requested.v1",
		Payload: fmt.Sprintf(
			`{"schema_version":1,"type":"generate_document_pdf","aggregate_type":"invoice","aggregate_id":%q,"invoice_id":%q,"document_id":%q,"invoice_version":7,"render_job_id":%q}`,
			invoiceID,
			invoiceID,
			invoiceID,
			renderJobID,
		),
	}, invoiceID, renderJobID
}

func validIssuedOutboxEvent() (*models.OutboxEvent, string, string) {
	invoiceID := uuid.NewString()
	renderJobID := uuid.NewString()
	return &models.OutboxEvent{
		ID:            uuid.NewString(),
		BusinessID:    uuid.NewString(),
		AggregateType: "invoice",
		AggregateID:   invoiceID,
		EventType:     "invoice.issued.v1",
		Payload: fmt.Sprintf(
			`{"schema_version":1,"aggregate_type":"invoice","aggregate_id":%q,"invoice_no":"INV/1","invoice_version":8,"render_job_id":%q}`,
			invoiceID,
			renderJobID,
		),
	}, invoiceID, renderJobID
}

func cloneOutboxEvent(event *models.OutboxEvent) *models.OutboxEvent {
	cloned := *event
	return &cloned
}
