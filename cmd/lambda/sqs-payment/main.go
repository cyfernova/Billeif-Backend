package main

import (
	"context"
	"fmt"
	"sync"

	"invoice-backend/internal/app"
	"invoice-backend/internal/workers"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

var (
	paymentInitOnce sync.Once
	paymentRT       *app.Runtime
	paymentInitErr  error
)

func initPaymentRuntime() {
	ctx := context.Background()
	paymentRT, paymentInitErr = app.Initialize(ctx, app.InitializeOptions{EnableWorker: false})
	if paymentInitErr != nil {
		paymentInitErr = fmt.Errorf("initialize payment worker runtime: %w", paymentInitErr)
	}
}

func handleSQSEvent(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
	paymentInitOnce.Do(initPaymentRuntime)
	if paymentInitErr != nil {
		return events.SQSEventResponse{}, paymentInitErr
	}

	failures := make([]events.SQSBatchItemFailure, 0)
	for _, record := range event.Records {
		if err := workers.ProcessPaymentQueueMessage(ctx, paymentRT.Log, record.Body); err != nil {
			paymentRT.Log.Error("failed to process payment queue record", "message_id", record.MessageId, "error", err)
			failures = append(failures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}

	return events.SQSEventResponse{BatchItemFailures: failures}, nil
}

func main() {
	lambda.Start(handleSQSEvent)
}
