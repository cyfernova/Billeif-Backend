package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
)

type bulkImportHandlerProcessorFake struct {
	err   error
	calls int
}

func (f *bulkImportHandlerProcessorFake) ProcessBulkImportBatch(context.Context, string, string, string, string, int) (bool, error) {
	f.calls++
	return false, f.err
}

func TestProcessSQSEventUsesPartialBatchFailures(t *testing.T) {
	processor := &bulkImportHandlerProcessorFake{err: errors.New("retry")}
	id := uuid.NewString()
	response, err := processSQSEvent(context.Background(), events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "failed", Body: `{"business_id":"` + id + `","job_id":"` + uuid.NewString() + `","commit_command_id":"` + uuid.NewString() + `"}`},
		{MessageId: "malformed", Body: `not-json`},
	}}, processor, "request-id", nil)
	if err != nil {
		t.Fatalf("processSQSEvent() error = %v", err)
	}
	if len(response.BatchItemFailures) != 2 || response.BatchItemFailures[0].ItemIdentifier != "failed" || response.BatchItemFailures[1].ItemIdentifier != "malformed" {
		t.Fatalf("batch failures = %#v", response.BatchItemFailures)
	}
	if processor.calls != 1 {
		t.Fatalf("processor calls = %d, want 1", processor.calls)
	}
}

func TestInitBulkImportRuntimeUsesScopedProfile(t *testing.T) {
	original := initializeBulkImport
	defer func() { initializeBulkImport = original }()
	var captured app.InitializeOptions
	wantRuntime := &app.Runtime{}
	initializeBulkImport = func(_ context.Context, options app.InitializeOptions) (*app.Runtime, error) {
		captured = options
		return wantRuntime, nil
	}

	bulkImportRT, bulkImportInitErr = nil, nil
	initBulkImportRuntime()
	if bulkImportInitErr != nil || bulkImportRT != wantRuntime {
		t.Fatalf("runtime/error = %#v/%v", bulkImportRT, bulkImportInitErr)
	}
	if captured.Profile != config.ProfileBulkImport || captured.EnableWorker {
		t.Fatalf("initialize options = %#v", captured)
	}
}

func TestInitBulkImportRuntimeWrapsInitializationFailure(t *testing.T) {
	original := initializeBulkImport
	defer func() { initializeBulkImport = original }()
	initializeBulkImport = func(context.Context, app.InitializeOptions) (*app.Runtime, error) {
		return nil, errors.New("database unavailable")
	}

	bulkImportRT, bulkImportInitErr = nil, nil
	initBulkImportRuntime()
	if bulkImportInitErr == nil || !strings.Contains(bulkImportInitErr.Error(), "initialize bulk import worker runtime") {
		t.Fatalf("initialization error = %v", bulkImportInitErr)
	}
}
