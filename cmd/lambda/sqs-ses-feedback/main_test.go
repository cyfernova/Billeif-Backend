package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

type recordingFeedbackProcessor struct {
	bodies []string
	fail   string
}

func (p *recordingFeedbackProcessor) Process(_ context.Context, body string) error {
	p.bodies = append(p.bodies, body)
	if body == p.fail {
		return fmt.Errorf("retry")
	}
	return nil
}

func TestHandlerReturnsOnlyFailedRawSNSRecords(t *testing.T) {
	processor := &recordingFeedbackProcessor{fail: `{"eventType":"Bounce"}`}
	handler := feedbackHandler{processor: processor}

	response, err := handler.Handle(context.Background(), events.SQSEvent{
		Records: []events.SQSMessage{
			{MessageId: "delivery", Body: `{"eventType":"Delivery"}`},
			{MessageId: "bounce", Body: `{"eventType":"Bounce"}`},
			{MessageId: "complaint", Body: `{"eventType":"Complaint"}`},
		},
	})

	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(response.BatchItemFailures) != 1 ||
		response.BatchItemFailures[0].ItemIdentifier != "bounce" {
		t.Fatalf("batch failures = %#v", response.BatchItemFailures)
	}
	if len(processor.bodies) != 3 || processor.bodies[0] != `{"eventType":"Delivery"}` {
		t.Fatalf("raw bodies = %#v", processor.bodies)
	}
}

func TestHandlerRejectsMissingMessageIDForWholeInvocation(t *testing.T) {
	processor := &recordingFeedbackProcessor{}
	handler := feedbackHandler{processor: processor}

	response, err := handler.Handle(context.Background(), events.SQSEvent{
		Records: []events.SQSMessage{
			{MessageId: "valid", Body: `{}`},
			{Body: `{}`},
		},
	})

	if err == nil || len(response.BatchItemFailures) != 0 || len(processor.bodies) != 0 {
		t.Fatalf("response/error/bodies = %#v/%v/%#v", response, err, processor.bodies)
	}
}
