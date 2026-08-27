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
	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/google/uuid"
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
		Profile:      config.ProfileInvoice,
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
	return processSQSEvent(ctx, event, func(ctx context.Context, body, owner string) error {
		return workers.ProcessInvoiceQueueMessageWithOwner(ctx, invoiceRT.Config, invoiceRT.Svcs, invoiceRT.Log, body, owner)
	}, invoiceRT.Log), nil
}

type invoiceMessageProcessor func(context.Context, string, string) error

func processSQSEvent(
	ctx context.Context,
	event events.SQSEvent,
	process invoiceMessageProcessor,
	log interface {
		Error(string, ...interface{})
	},
) events.SQSEventResponse {
	failures := make([]events.SQSBatchItemFailure, 0)
	requestID := ""
	if lambdaContext, ok := lambdacontext.FromContext(ctx); ok {
		requestID = lambdaContext.AwsRequestID
	}
	if requestID == "" {
		requestID = uuid.NewString()
	}
	for _, record := range event.Records {
		owner := workers.NewInvoiceRenderLeaseOwner(requestID, record.MessageId)
		if err := process(ctx, record.Body, owner); err != nil {
			log.Error("failed to process invoice queue record", "message_id", record.MessageId, "error", err)
			failures = append(failures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}

	return events.SQSEventResponse{BatchItemFailures: failures}
}

func main() {
	lambda.Start(handleSQSEvent)
}
