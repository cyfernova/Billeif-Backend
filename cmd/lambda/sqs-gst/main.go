package main

import (
	"context"
	"fmt"
	"sync"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"
	"invoice-backend/internal/workers"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/operationsmetrics"

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
		Profile:      config.ProfileGST,
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
	failures := make([]events.SQSBatchItemFailure, 0)
	for _, record := range event.Records {
		processErr := workers.ProcessGSTQueueMessage(ctx, gstRT.Svcs, gstRT.Log, record.Body)
		emitGSTRecordMetrics(gstRT.Metrics, record, processErr != nil, gstRT.Log)
		if processErr != nil {
			gstRT.Log.Error("failed to process gst queue record", "message_id", record.MessageId, "error", processErr)
			failures = append(failures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}

	return events.SQSEventResponse{BatchItemFailures: failures}, nil
}

// emitGSTRecordMetrics reports the bounded outcome of one processed GST
// record: the observed queue age and, when the record failed after SQS
// already redelivered it, one recovery sample. GST records never invent a
// render or delivery rate. Provider call latency is emitted inside the GST
// provider itself.
func emitGSTRecordMetrics(
	metrics *operationsmetrics.Emitter,
	record events.SQSMessage,
	failed bool,
	log *logger.Logger,
) {
	if metrics == nil {
		return
	}
	if age, ok := metrics.QueueAgeSeconds(record.Attributes); ok {
		if err := metrics.Emit(operationsmetrics.Sample{
			Category: operationsmetrics.CategoryProvider,
			Values:   map[operationsmetrics.Metric]float64{operationsmetrics.MetricQueueAgeSeconds: age},
		}); err != nil {
			log.Warn("emit gst queue age metric", "error", err)
		}
	}
	if !failed {
		return
	}
	if count, ok := operationsmetrics.SQSReceiveCount(record.Attributes); !ok || count < 2 {
		return
	}
	if err := metrics.EmitRepeatedFailure(); err != nil {
		log.Warn("emit gst repeated failure metric", "error", err)
	}
}

func main() {
	lambda.Start(handleSQSEvent)
}
