package outbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"invoice-backend/internal/models"

	"github.com/google/uuid"
)

const (
	invoicePreviewRequestedEvent  = "invoice.preview.requested.v1"
	invoiceIssuedEvent            = "invoice.issued.v1"
	invoiceDeliveryRequestedEvent = "invoice.delivery.requested.v1"
	generateDocumentPDFMessage    = "generate_document_pdf"
	sendInvoicePDFMessage         = "send_invoice_pdf"
)

type InvoiceEventMappingError struct {
	EventID string
}

func (e *InvoiceEventMappingError) Error() string {
	if e.EventID == "" {
		return "invoice outbox event mapping failed"
	}
	return fmt.Sprintf("invoice outbox event %q mapping failed", e.EventID)
}

type invoiceWorkerMessage struct {
	Type           string `json:"type"`
	InvoiceID      string `json:"invoice_id"`
	DocumentID     string `json:"document_id"`
	InvoiceVersion int    `json:"invoice_version"`
	RenderJobID    string `json:"render_job_id"`
}

type invoicePreviewEnvelope struct {
	SchemaVersion  int    `json:"schema_version"`
	Type           string `json:"type"`
	AggregateType  string `json:"aggregate_type"`
	AggregateID    string `json:"aggregate_id"`
	InvoiceID      string `json:"invoice_id"`
	DocumentID     string `json:"document_id"`
	InvoiceVersion int    `json:"invoice_version"`
	RenderJobID    string `json:"render_job_id"`
}

type invoiceIssuedEnvelope struct {
	SchemaVersion  int    `json:"schema_version"`
	AggregateType  string `json:"aggregate_type"`
	AggregateID    string `json:"aggregate_id"`
	InvoiceNo      string `json:"invoice_no"`
	InvoiceVersion int    `json:"invoice_version"`
	RenderJobID    string `json:"render_job_id"`
}

type invoiceDeliveryEnvelope struct {
	SchemaVersion int    `json:"schema_version"`
	DeliveryID    string `json:"delivery_id"`
	InvoiceID     string `json:"invoice_id"`
	RenderJobID   string `json:"render_job_id"`
}

type emailDeliveryWorkerMessage struct {
	SchemaVersion int    `json:"schema_version"`
	Type          string `json:"type"`
	BusinessID    string `json:"business_id"`
	DeliveryID    string `json:"delivery_id"`
	InvoiceID     string `json:"invoice_id"`
	RenderJobID   string `json:"render_job_id"`
}

func MapInvoiceEventToSQSMessage(event *models.OutboxEvent) ([]byte, error) {
	if !validInvoiceOutboxIdentity(event) {
		return nil, invoiceMappingError(event)
	}

	var message invoiceWorkerMessage
	switch event.EventType {
	case invoicePreviewRequestedEvent:
		var envelope invoicePreviewEnvelope
		if err := decodeExactJSON(event.Payload, &envelope); err != nil ||
			envelope.SchemaVersion != 1 ||
			envelope.Type != generateDocumentPDFMessage ||
			envelope.AggregateType != event.AggregateType ||
			envelope.AggregateID != event.AggregateID ||
			envelope.InvoiceID != event.AggregateID ||
			envelope.DocumentID != event.AggregateID ||
			envelope.InvoiceVersion < 1 ||
			!validUUID(envelope.RenderJobID) {
			return nil, invoiceMappingError(event)
		}
		message = invoiceWorkerMessage{
			Type:           generateDocumentPDFMessage,
			InvoiceID:      envelope.InvoiceID,
			DocumentID:     envelope.DocumentID,
			InvoiceVersion: envelope.InvoiceVersion,
			RenderJobID:    envelope.RenderJobID,
		}
	case invoiceIssuedEvent:
		var envelope invoiceIssuedEnvelope
		if err := decodeExactJSON(event.Payload, &envelope); err != nil ||
			envelope.SchemaVersion != 1 ||
			envelope.AggregateType != event.AggregateType ||
			envelope.AggregateID != event.AggregateID ||
			envelope.InvoiceVersion < 1 ||
			!validUUID(envelope.RenderJobID) {
			return nil, invoiceMappingError(event)
		}
		message = invoiceWorkerMessage{
			Type:           generateDocumentPDFMessage,
			InvoiceID:      event.AggregateID,
			DocumentID:     event.AggregateID,
			InvoiceVersion: envelope.InvoiceVersion,
			RenderJobID:    envelope.RenderJobID,
		}
	default:
		return nil, invoiceMappingError(event)
	}

	mapped, err := json.Marshal(message)
	if err != nil {
		return nil, invoiceMappingError(event)
	}
	return mapped, nil
}

func MapEmailDeliveryEventToSQSMessage(event *models.OutboxEvent) ([]byte, error) {
	if event == nil ||
		!validUUID(event.ID) ||
		!validUUID(event.BusinessID) ||
		event.AggregateType != "email_delivery" ||
		!validUUID(event.AggregateID) ||
		event.EventType != invoiceDeliveryRequestedEvent ||
		strings.TrimSpace(event.Payload) == "" {
		return nil, invoiceMappingError(event)
	}
	var envelope invoiceDeliveryEnvelope
	if err := decodeExactJSON(event.Payload, &envelope); err != nil ||
		envelope.SchemaVersion != 1 ||
		envelope.DeliveryID != event.AggregateID ||
		!validUUID(envelope.DeliveryID) ||
		!validUUID(envelope.InvoiceID) ||
		!validUUID(envelope.RenderJobID) {
		return nil, invoiceMappingError(event)
	}
	message, err := json.Marshal(emailDeliveryWorkerMessage{
		SchemaVersion: 1,
		Type:          sendInvoicePDFMessage,
		BusinessID:    event.BusinessID,
		DeliveryID:    envelope.DeliveryID,
		InvoiceID:     envelope.InvoiceID,
		RenderJobID:   envelope.RenderJobID,
	})
	if err != nil {
		return nil, invoiceMappingError(event)
	}
	return message, nil
}

func validInvoiceOutboxIdentity(event *models.OutboxEvent) bool {
	return event != nil &&
		validUUID(event.ID) &&
		validUUID(event.BusinessID) &&
		event.AggregateType == "invoice" &&
		validUUID(event.AggregateID) &&
		strings.TrimSpace(event.Payload) != ""
}

func validUUID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}

func decodeExactJSON(payload string, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewBufferString(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("payload contains trailing JSON")
	}
	return nil
}

func invoiceMappingError(event *models.OutboxEvent) error {
	if event == nil {
		return &InvoiceEventMappingError{}
	}
	return &InvoiceEventMappingError{EventID: event.ID}
}
