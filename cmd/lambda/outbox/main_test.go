package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/outbox"

	"github.com/aws/aws-lambda-go/lambdacontext"
)

type recordingDispatcher struct {
	owner  string
	result outbox.DispatchResult
	err    error
}

func (d *recordingDispatcher) Dispatch(_ context.Context, owner string) (outbox.DispatchResult, error) {
	d.owner = owner
	return d.result, d.err
}

func TestHandlerUsesLambdaRequestIDAsLeaseOwner(t *testing.T) {
	dispatcher := &recordingDispatcher{result: outbox.DispatchResult{Claimed: 2, Published: 2}}
	handler := lambdaHandler{dispatcher: dispatcher}
	ctx := lambdacontext.NewContext(context.Background(), &lambdacontext.LambdaContext{
		AwsRequestID: "billeif-request-123",
	})

	result, err := handler.Handle(ctx)

	if err != nil {
		t.Fatalf("handle outbox schedule: %v", err)
	}
	if dispatcher.owner != "billeif-request-123" {
		t.Fatalf("lease owner = %q", dispatcher.owner)
	}
	if result.Published != 2 {
		t.Fatalf("dispatch result = %#v", result)
	}
}

func TestHandlerRejectsMissingLambdaRequestID(t *testing.T) {
	handler := lambdaHandler{dispatcher: &recordingDispatcher{}}

	_, err := handler.Handle(context.Background())

	if err == nil || !strings.Contains(err.Error(), "request ID") {
		t.Fatalf("missing request ID error = %v", err)
	}
}

type recordingDatabasePool struct {
	maxOpen     int
	maxIdle     int
	maxIdleTime time.Duration
	maxLifetime time.Duration
}

func (p *recordingDatabasePool) SetMaxOpenConns(value int) {
	p.maxOpen = value
}

func (p *recordingDatabasePool) SetMaxIdleConns(value int) {
	p.maxIdle = value
}

func (p *recordingDatabasePool) SetConnMaxIdleTime(value time.Duration) {
	p.maxIdleTime = value
}

func (p *recordingDatabasePool) SetConnMaxLifetime(value time.Duration) {
	p.maxLifetime = value
}

func TestConfigureDatabasePoolBoundsOutboxLambdaConnections(t *testing.T) {
	pool := &recordingDatabasePool{}

	configureDatabasePool(pool)

	if pool.maxOpen != 1 || pool.maxIdle != 0 ||
		pool.maxIdleTime != 2*time.Minute || pool.maxLifetime != 10*time.Minute {
		t.Fatalf("database pool settings = %#v", pool)
	}
}
