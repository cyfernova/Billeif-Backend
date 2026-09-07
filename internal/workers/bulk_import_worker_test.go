package workers

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
)

type bulkImportProcessorFake struct {
	err        error
	calls      int
	business   string
	job        string
	command    string
	owner      string
	batch      int
	cleanups   int
	recoveries int
}

func (f *bulkImportProcessorFake) CleanupExpired(context.Context, int) (int, error) {
	f.cleanups++
	return 0, nil
}

func (f *bulkImportProcessorFake) RecoverPending(context.Context, int) (int, error) {
	f.recoveries++
	return 0, nil
}

func (f *bulkImportProcessorFake) ProcessBulkImportBatch(
	_ context.Context,
	businessID,
	jobID,
	commandID,
	owner string,
	batch int,
) (bool, error) {
	f.calls++
	f.business, f.job, f.command, f.owner, f.batch = businessID, jobID, commandID, owner, batch
	return false, f.err
}

type bulkImportLoggerFake struct{ calls int }

func (f *bulkImportLoggerFake) Error(string, ...interface{}) { f.calls++ }

func TestProcessBulkImportQueueMessageBindsDurableCommandAndOwner(t *testing.T) {
	processor := &bulkImportProcessorFake{}
	businessID, jobID, commandID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	body := `{"business_id":"` + businessID + `","job_id":"` + jobID + `","commit_command_id":"` + commandID + `"}`

	if err := ProcessBulkImportQueueMessage(context.Background(), processor, body, "request:message"); err != nil {
		t.Fatalf("ProcessBulkImportQueueMessage() error = %v", err)
	}
	if processor.calls != 1 || processor.business != businessID || processor.job != jobID || processor.command != commandID || processor.owner != "request:message" || processor.batch != 100 {
		t.Fatalf("processor call = %#v", processor)
	}
}

func TestProcessBulkImportQueueMessageRejectsMalformedOrExpandedCommands(t *testing.T) {
	processor := &bulkImportProcessorFake{}
	validID := uuid.NewString()
	for _, body := range []string{
		`not-json`,
		`{"business_id":"` + validID + `","job_id":"bad","commit_command_id":"` + validID + `"}`,
		`{"business_id":"` + validID + `","job_id":"` + validID + `","commit_command_id":"` + validID + `","other":true}`,
		`{"business_id":"` + validID + `","job_id":"` + validID + `","commit_command_id":"` + validID + `"}{}`,
	} {
		if err := ProcessBulkImportQueueMessage(context.Background(), processor, body, "owner"); !errors.Is(err, ErrInvalidBulkImportQueueMessage) {
			t.Fatalf("body %q error = %v", body, err)
		}
	}
	if processor.calls != 0 {
		t.Fatalf("invalid commands reached processor %d times", processor.calls)
	}
}

func TestProcessBulkImportSQSEventReturnsOnlyFailedRecordIdentifiers(t *testing.T) {
	processor := &bulkImportProcessorFake{err: errors.New("database unavailable")}
	log := &bulkImportLoggerFake{}
	validID := uuid.NewString()
	valid := `{"business_id":"` + validID + `","job_id":"` + uuid.NewString() + `","commit_command_id":"` + uuid.NewString() + `"}`

	response, err := ProcessBulkImportSQSEvent(context.Background(), events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "retry-service", Body: valid},
		{MessageId: "retry-malformed", Body: `not-json`},
	}}, processor, "request-id", log)
	if err != nil {
		t.Fatalf("ProcessBulkImportSQSEvent() error = %v", err)
	}
	if len(response.BatchItemFailures) != 2 || response.BatchItemFailures[0].ItemIdentifier != "retry-service" || response.BatchItemFailures[1].ItemIdentifier != "retry-malformed" {
		t.Fatalf("batch failures = %#v", response.BatchItemFailures)
	}
	if processor.owner != "request-id:retry-service" || log.calls != 2 {
		t.Fatalf("owner/log calls = %q/%d", processor.owner, log.calls)
	}
}

func TestProcessBulkImportSQSEventCanResumeSameCommandAfterRestart(t *testing.T) {
	processor := &bulkImportProcessorFake{}
	validID := uuid.NewString()
	event := events.SQSEvent{Records: []events.SQSMessage{{
		MessageId: "delivery", Body: `{"business_id":"` + validID + `","job_id":"` + uuid.NewString() + `","commit_command_id":"` + uuid.NewString() + `"}`,
	}}}

	for _, requestID := range []string{"cold-start-one", "cold-start-two"} {
		response, err := ProcessBulkImportSQSEvent(context.Background(), event, processor, requestID, nil)
		if err != nil || len(response.BatchItemFailures) != 0 {
			t.Fatalf("restart %q response/error = %#v/%v", requestID, response, err)
		}
	}
	if processor.calls != 2 || processor.cleanups != 2 || processor.recoveries != 2 || processor.owner != "cold-start-two:delivery" {
		t.Fatalf("restart-safe calls/cleanup/recovery/owner = %d/%d/%d/%q", processor.calls, processor.cleanups, processor.recoveries, processor.owner)
	}
}
