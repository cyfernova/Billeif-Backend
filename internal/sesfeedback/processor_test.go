package sesfeedback

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type recordingFeedbackRepository struct {
	events []FeedbackEvent
	err    error
}

func (r *recordingFeedbackRepository) ApplyFeedback(
	_ context.Context,
	event FeedbackEvent,
) (ApplyResult, error) {
	r.events = append(r.events, event)
	return ApplyResult{Status: "delivered", Changed: true}, r.err
}

func TestProcessorConsumesRawSNSMessageBody(t *testing.T) {
	repository := &recordingFeedbackRepository{}
	processor, err := NewProcessor(ProcessorOptions{
		Repository: repository,
		Validation: ValidationOptions{
			SendingAccountID: "123456789012",
			ConfigurationSet: "Billeif-prod-ses-config",
		},
	})
	if err != nil {
		t.Fatalf("new processor: %v", err)
	}
	businessID, deliveryID := uuid.NewString(), uuid.NewString()

	err = processor.Process(
		context.Background(),
		validFeedbackJSON("Delivery", businessID, deliveryID, "provider-message-1"),
	)

	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(repository.events) != 1 ||
		repository.events[0].DeliveryID != deliveryID ||
		repository.events[0].BusinessID != businessID {
		t.Fatalf("repository events = %#v", repository.events)
	}
}

func TestProcessorReturnsParsingAndRepositoryFailuresForSQSRetry(t *testing.T) {
	repository := &recordingFeedbackRepository{err: errors.New("database unavailable")}
	processor, err := NewProcessor(ProcessorOptions{
		Repository: repository,
		Validation: ValidationOptions{
			SendingAccountID: "123456789012",
			ConfigurationSet: "Billeif-prod-ses-config",
		},
	})
	if err != nil {
		t.Fatalf("new processor: %v", err)
	}
	if err := processor.Process(context.Background(), `{"Type":"Notification"}`); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("enveloped message error = %v", err)
	}
	body := validFeedbackJSON("Delivery", uuid.NewString(), uuid.NewString(), "provider-message-1")
	if err := processor.Process(context.Background(), body); !errors.Is(err, repository.err) {
		t.Fatalf("repository error = %v", err)
	}
}
