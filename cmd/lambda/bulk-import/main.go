package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"
	"invoice-backend/internal/workers"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/google/uuid"
)

var errBulkImportRuntimeUnavailable = errors.New("bulk import worker runtime is unavailable")

var (
	bulkImportInitOnce   sync.Once
	bulkImportRT         *app.Runtime
	bulkImportInitErr    error
	initializeBulkImport = app.Initialize
)

func initBulkImportRuntime() {
	bulkImportRT, bulkImportInitErr = initializeBulkImport(context.Background(), app.InitializeOptions{
		EnableWorker: false,
		Profile:      config.ProfileBulkImport,
	})
	if bulkImportInitErr != nil {
		bulkImportInitErr = fmt.Errorf("initialize bulk import worker runtime: %w", bulkImportInitErr)
	}
}

func handleSQSEvent(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
	bulkImportInitOnce.Do(initBulkImportRuntime)
	if bulkImportInitErr != nil {
		return events.SQSEventResponse{}, bulkImportInitErr
	}
	if bulkImportRT == nil || bulkImportRT.Svcs == nil || bulkImportRT.Svcs.BulkImport == nil {
		return events.SQSEventResponse{}, errBulkImportRuntimeUnavailable
	}
	requestID := ""
	if lambdaContext, ok := lambdacontext.FromContext(ctx); ok {
		requestID = lambdaContext.AwsRequestID
	}
	if requestID == "" {
		requestID = uuid.NewString()
	}
	return processSQSEvent(ctx, event, bulkImportRT.Svcs.BulkImport, requestID, bulkImportRT.Log)
}

func processSQSEvent(
	ctx context.Context,
	event events.SQSEvent,
	processor workers.BulkImportProcessor,
	requestID string,
	log *logger.Logger,
) (events.SQSEventResponse, error) {
	if log == nil {
		return workers.ProcessBulkImportSQSEvent(ctx, event, processor, requestID, nil)
	}
	return workers.ProcessBulkImportSQSEvent(ctx, event, processor, requestID, log)
}

func main() {
	lambda.Start(handleSQSEvent)
}
