package main

import (
	"context"
	"fmt"
	"sync"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"
	"invoice-backend/internal/workers"
	"invoice-backend/pkg/operationsmetrics"

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
	}, invoiceRT.Metrics, invoiceRT.Log), nil
}

type invoiceMessageProcessor func(context.Context, string, string) error

func processSQSEvent(
	ctx context.Context,
	event events.SQSEvent,
	process invoiceMessageProcessor,
	metrics *operationsmetrics.Emitter,
	log interface {
		Error(string, ...interface{})
		Warn(string, ...interface{})
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
		err := process(ctx, record.Body, owner)
		emitInvoiceRecordMetrics(metrics, record, err != nil, log)
		if err != nil {
			log.Error("failed to process invoice queue record", "message_id", record.MessageId, "error", err)
			failures = append(failures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}

	return events.SQSEventResponse{BatchItemFailures: failures}
}

// emitInvoiceRecordMetrics reports the bounded outcome of one processed render
// record. Telemetry failures never turn into record failures.
func emitInvoiceRecordMetrics(
	metrics *operationsmetrics.Emitter,
	record events.SQSMessage,
	failed bool,
	log interface{ Warn(string, ...interface{}) },
) {
	if metrics == nil {
		return
	}
	if err := metrics.EmitRecordOutcome(operationsmetrics.RecordOutcome{
		Category: operationsmetrics.CategoryRender, RateMetric: operationsmetrics.MetricRenderFailureRate,
		Failed: failed, Attributes: record.Attributes,
	}); err != nil {
		log.Warn("emit render outcome metric", "error", err)
	}
}

func main() {
	lambda.Start(handleSQSEvent)
}
