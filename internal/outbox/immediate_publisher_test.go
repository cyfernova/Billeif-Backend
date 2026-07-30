package outbox

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"invoice-backend/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
)

type sqsSenderFake struct {
	calls int
	input *sqs.SendMessageInput
	err   error
}

func (s *sqsSenderFake) SendMessage(
	_ context.Context,
	input *sqs.SendMessageInput,
	_ ...func(*sqs.Options),
) (*sqs.SendMessageOutput, error) {
	s.calls++
	s.input = input
	if s.err != nil {
		return nil, s.err
	}
	return &sqs.SendMessageOutput{MessageId: aws.String("message-1")}, nil
}

type publishedMarkerFake struct {
	calls     int
	eventID   string
	published time.Time
	err       error
}

func (m *publishedMarkerFake) MarkOutboxPublished(
	_ context.Context,
	eventID string,
	published time.Time,
) error {
	m.calls++
	m.eventID = eventID
	m.published = published
	return m.err
}

func TestImmediatePublisherSendsMappedMessageThenMarksExactEvent(t *testing.T) {
	sender := &sqsSenderFake{}
	marker := &publishedMarkerFake{}
	publisher := NewImmediatePublisher("invoice-queue-url", sender, marker)
	event, invoiceID, renderJobID := validIssuedOutboxEvent()

	err := publisher.TryPublish(context.Background(), event)

	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if sender.calls != 1 || sender.input == nil ||
		aws.ToString(sender.input.QueueUrl) != "invoice-queue-url" {
		t.Fatalf("send input = %#v", sender.input)
	}
	wantMessage := fmt.Sprintf(
		`{"type":"generate_document_pdf","invoice_id":%q,"document_id":%q,"invoice_version":8,"render_job_id":%q}`,
		invoiceID,
		invoiceID,
		renderJobID,
	)
	if aws.ToString(sender.input.MessageBody) != wantMessage {
		t.Fatalf("message body = %s, want %s", aws.ToString(sender.input.MessageBody), wantMessage)
	}
	if marker.calls != 1 || marker.eventID != event.ID || marker.published.IsZero() {
		t.Fatalf("marker calls/event/time = %d/%q/%v", marker.calls, marker.eventID, marker.published)
	}
}

func TestImmediatePublisherSendFailureLeavesEventPending(t *testing.T) {
	sender := &sqsSenderFake{err: errors.New("send failed")}
	marker := &publishedMarkerFake{}
	publisher := NewImmediatePublisher("invoice-queue-url", sender, marker)
	event, _, _ := validPreviewOutboxEvent()

	err := publisher.TryPublish(context.Background(), event)

	if err == nil {
		t.Fatal("send failure returned nil")
	}
	if marker.calls != 0 {
		t.Fatalf("marker calls = %d, want 0 after send failure", marker.calls)
	}
}

func TestImmediatePublisherMarkFailureRemainsRecoverable(t *testing.T) {
	sender := &sqsSenderFake{}
	marker := &publishedMarkerFake{err: errors.New("mark failed")}
	publisher := NewImmediatePublisher("invoice-queue-url", sender, marker)
	event, _, _ := validPreviewOutboxEvent()

	err := publisher.TryPublish(context.Background(), event)

	if err == nil {
		t.Fatal("mark failure returned nil")
	}
	err = publisher.TryPublish(context.Background(), event)
	if err == nil {
		t.Fatal("duplicate mark failure returned nil")
	}
	if sender.calls != 2 || marker.calls != 2 || marker.eventID != event.ID {
		t.Fatalf("send/mark/event = %d/%d/%q", sender.calls, marker.calls, marker.eventID)
	}
}

func TestImmediatePublisherMappingFailureDoesNotSendOrMark(t *testing.T) {
	sender := &sqsSenderFake{}
	marker := &publishedMarkerFake{}
	publisher := NewImmediatePublisher("invoice-queue-url", sender, marker)
	event, _, _ := validIssuedOutboxEvent()
	event.AggregateID = uuid.NewString()

	err := publisher.TryPublish(context.Background(), event)

	var mappingError *InvoiceEventMappingError
	if !errors.As(err, &mappingError) {
		t.Fatalf("publish error = %T %v, want typed mapping error", err, err)
	}
	if sender.calls != 0 || marker.calls != 0 {
		t.Fatalf("map failure send/mark calls = %d/%d, want 0/0", sender.calls, marker.calls)
	}
}

func TestImmediatePublisherRejectsMissingDependenciesWithoutNetwork(t *testing.T) {
	fixtures := []struct {
		name      string
		publisher *ImmediatePublisher
		event     *models.OutboxEvent
	}{
		{name: "empty queue", publisher: NewImmediatePublisher("", &sqsSenderFake{}, &publishedMarkerFake{}), event: &models.OutboxEvent{ID: uuid.NewString()}},
		{name: "nil sender", publisher: NewImmediatePublisher("queue", nil, &publishedMarkerFake{}), event: &models.OutboxEvent{ID: uuid.NewString()}},
		{name: "nil marker", publisher: NewImmediatePublisher("queue", &sqsSenderFake{}, nil), event: &models.OutboxEvent{ID: uuid.NewString()}},
		{name: "nil event", publisher: NewImmediatePublisher("queue", &sqsSenderFake{}, &publishedMarkerFake{})},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			if err := fixture.publisher.TryPublish(context.Background(), fixture.event); err == nil {
				t.Fatal("invalid publisher input returned nil")
			}
		})
	}
}
