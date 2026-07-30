package outbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type SQSSender interface {
	SendMessage(
		ctx context.Context,
		input *sqs.SendMessageInput,
		optFns ...func(*sqs.Options),
	) (*sqs.SendMessageOutput, error)
}

type PublishedMarker interface {
	MarkOutboxPublished(ctx context.Context, eventID string, publishedAt time.Time) error
}

type ImmediatePublisher struct {
	queueURL string
	sender   SQSSender
	marker   PublishedMarker
}

func NewImmediatePublisher(
	queueURL string,
	sender SQSSender,
	marker PublishedMarker,
) *ImmediatePublisher {
	return &ImmediatePublisher{
		queueURL: strings.TrimSpace(queueURL),
		sender:   sender,
		marker:   marker,
	}
}

func (p *ImmediatePublisher) TryPublish(ctx context.Context, event *models.OutboxEvent) error {
	if p == nil || p.queueURL == "" || p.sender == nil || p.marker == nil ||
		event == nil || event.ID == "" || event.Payload == "" {
		return errors.New("immediate outbox publisher is not configured")
	}
	if _, err := p.sender.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(p.queueURL),
		MessageBody: aws.String(event.Payload),
	}); err != nil {
		return fmt.Errorf("send outbox event: %w", err)
	}
	if err := p.marker.MarkOutboxPublished(ctx, event.ID, time.Now().UTC()); err != nil {
		return fmt.Errorf("mark outbox event published: %w", err)
	}
	return nil
}
