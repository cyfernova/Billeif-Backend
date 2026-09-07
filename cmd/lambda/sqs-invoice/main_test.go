package main

import (
	"context"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/internal/services"
	"invoice-backend/internal/workers"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/events"
)

func TestProcessSQSEventAcknowledgesRetiredMessagesAndRetriesErrors(t *testing.T) {
	log := logger.NewWithEnv("test")
	process := func(ctx context.Context, body, owner string) error {
		return workers.ProcessInvoiceQueueMessageWithOwner(
			ctx,
			&config.Config{},
			&services.Container{},
			log,
			body,
			owner,
		)
	}

	response := processSQSEvent(context.Background(), events.SQSEvent{
		Records: []events.SQSMessage{
			{MessageId: "retired", Body: `{"type":"generate_pdf","invoice_id":"legacy-invoice"}`},
			{MessageId: "malformed", Body: "not-json"},
		},
	}, process, nil, log)

	if len(response.BatchItemFailures) != 1 ||
		response.BatchItemFailures[0].ItemIdentifier != "malformed" {
		t.Fatalf("batch failures = %#v, want only malformed record", response.BatchItemFailures)
	}
}
