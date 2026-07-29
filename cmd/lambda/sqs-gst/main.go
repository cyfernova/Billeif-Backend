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
	gstInitOnce sync.Once
	gstRT       *app.Runtime
	gstInitErr  error
)

func initGSTRuntime() {
	ctx := context.Background()
	gstRT, gstInitErr = app.Initialize(ctx, app.InitializeOptions{
		EnableWorker: false,
		SecretKinds:  config.SecretKindsForEntrypoint("sqs-gst"),
	})
	if gstInitErr != nil {
		gstInitErr = fmt.Errorf("initialize gst worker runtime: %w", gstInitErr)
	}
}

func handleSQSEvent(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
	gstInitOnce.Do(initGSTRuntime)
	if gstInitErr != nil {
		return events.SQSEventResponse{}, gstInitErr
	}
	if err := gstRT.RefreshCredentials(ctx); err != nil {
		return events.SQSEventResponse{}, err
	}

	failures := make([]events.SQSBatchItemFailure, 0)
	for _, record := range event.Records {
		if err := workers.ProcessGSTQueueMessage(ctx, gstRT.Svcs, gstRT.Log, record.Body); err != nil {
			gstRT.Log.Error("failed to process gst queue record", "message_id", record.MessageId, "error", err)
			failures = append(failures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}

	return events.SQSEventResponse{BatchItemFailures: failures}, nil
}

func main() {
	lambda.Start(handleSQSEvent)
}
