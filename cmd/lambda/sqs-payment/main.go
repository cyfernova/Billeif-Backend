package main

import (
	"context"

	"invoice-backend/internal/workers"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

var paymentLog = logger.New().Named("payment_worker")

func handleSQSEvent(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
	failures := make([]events.SQSBatchItemFailure, 0)
	for _, record := range event.Records {
		if err := workers.ProcessPaymentQueueMessage(ctx, paymentLog, record.Body); err != nil {
			paymentLog.Error("failed to process payment queue record", "message_id", record.MessageId, "error", err)
			failures = append(failures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}

	return events.SQSEventResponse{BatchItemFailures: failures}, nil
}

func main() {
	lambda.Start(handleSQSEvent)
}
