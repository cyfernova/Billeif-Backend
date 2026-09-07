package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"invoice-backend/internal/services"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
)

const bulkImportWorkerBatchSize = 100

var ErrInvalidBulkImportQueueMessage = errors.New("invalid bulk import queue message")

type BulkImportProcessor interface {
	ProcessBulkImportBatch(context.Context, string, string, string, string, int) (bool, error)
}

type bulkImportCleaner interface {
	CleanupExpired(context.Context, int) (int, error)
}

type bulkImportRecoverer interface {
	RecoverPending(context.Context, int) (int, error)
}

type bulkImportWorkerLogger interface {
	Error(string, ...interface{})
}

func ProcessBulkImportQueueMessage(
	ctx context.Context,
	processor BulkImportProcessor,
	body,
	owner string,
) error {
	if processor == nil || strings.TrimSpace(owner) == "" {
		return ErrInvalidBulkImportQueueMessage
	}
	var message services.BulkImportQueueMessage
	decoder := json.NewDecoder(bytes.NewBufferString(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&message); err != nil {
		return fmt.Errorf("%w: decode: %v", ErrInvalidBulkImportQueueMessage, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing content", ErrInvalidBulkImportQueueMessage)
	}
	if uuid.Validate(message.BusinessID) != nil || uuid.Validate(message.JobID) != nil || uuid.Validate(message.CommitCommandID) != nil {
		return ErrInvalidBulkImportQueueMessage
	}
	_, err := processor.ProcessBulkImportBatch(
		ctx,
		message.BusinessID,
		message.JobID,
		message.CommitCommandID,
		owner,
		bulkImportWorkerBatchSize,
	)
	return err
}

func ProcessBulkImportSQSEvent(
	ctx context.Context,
	event events.SQSEvent,
	processor BulkImportProcessor,
	requestID string,
	log bulkImportWorkerLogger,
) (events.SQSEventResponse, error) {
	if processor == nil || strings.TrimSpace(requestID) == "" {
		return events.SQSEventResponse{}, ErrInvalidBulkImportQueueMessage
	}
	failures := make([]events.SQSBatchItemFailure, 0)
	for _, record := range event.Records {
		if strings.TrimSpace(record.MessageId) == "" {
			return events.SQSEventResponse{}, fmt.Errorf("%w: missing message id", ErrInvalidBulkImportQueueMessage)
		}
		owner := requestID + ":" + record.MessageId
		if err := ProcessBulkImportQueueMessage(ctx, processor, record.Body, owner); err != nil {
			if log != nil {
				log.Error("failed to process bulk import queue record", "message_id", record.MessageId, "error", err)
			}
			failures = append(failures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}
	var maintenanceErr error
	if recoverer, ok := processor.(bulkImportRecoverer); ok {
		if _, err := recoverer.RecoverPending(ctx, 100); err != nil {
			maintenanceErr = err
			if log != nil {
				log.Error("failed to recover pending bulk imports", "error", err)
			}
		}
	}
	if cleaner, ok := processor.(bulkImportCleaner); ok {
		if _, err := cleaner.CleanupExpired(ctx, 25); err != nil {
			if maintenanceErr == nil {
				maintenanceErr = err
			}
			if log != nil {
				log.Error("failed to clean expired bulk imports", "error", err)
			}
		}
	}
	if len(event.Records) == 0 && maintenanceErr != nil {
		return events.SQSEventResponse{}, maintenanceErr
	}
	return events.SQSEventResponse{BatchItemFailures: failures}, nil
}
