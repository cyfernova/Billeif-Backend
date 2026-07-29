package main

import (
	"context"
	"fmt"
	"sync"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"
	"invoice-backend/internal/workers"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

var (
	invoiceInitOnce sync.Once
	invoiceRT       *app.Runtime
	invoiceInitErr  error
)

func initInvoiceRuntime() {
	ctx := context.Background()
	invoiceRT, invoiceInitErr = app.Initialize(ctx, app.InitializeOptions{
		EnableWorker: false,
		SecretKinds:  config.SecretKindsForEntrypoint("sqs-invoice"),
	})
	if invoiceInitErr != nil {
		invoiceInitErr = fmt.Errorf("initialize invoice worker runtime: %w", invoiceInitErr)
	}
}

func handleSQSEvent(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
	invoiceInitOnce.Do(initInvoiceRuntime)
	if invoiceInitErr != nil {
		return events.SQSEventResponse{}, invoiceInitErr
	}
	if err := invoiceRT.RefreshCredentials(ctx); err != nil {
		return events.SQSEventResponse{}, err
	}

	failures := make([]events.SQSBatchItemFailure, 0)
	for _, record := range event.Records {
		if err := workers.ProcessInvoiceQueueMessage(ctx, invoiceRT.Config, invoiceRT.Svcs, invoiceRT.Log, record.Body); err != nil {
			invoiceRT.Log.Error("failed to process invoice queue record", "message_id", record.MessageId, "error", err)
			failures = append(failures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}

	return events.SQSEventResponse{BatchItemFailures: failures}, nil
}

func main() {
	lambda.Start(handleSQSEvent)
}
