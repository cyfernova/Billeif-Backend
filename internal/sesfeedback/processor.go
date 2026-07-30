package sesfeedback

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Repository interface {
	ApplyFeedback(context.Context, FeedbackEvent) (ApplyResult, error)
}

type ProcessorOptions struct {
	Repository Repository
	Validation ValidationOptions
}

type Processor struct {
	repository Repository
	validation ValidationOptions
}

func NewProcessor(options ProcessorOptions) (*Processor, error) {
	if options.Repository == nil {
		return nil, errors.New("SES feedback repository is required")
	}
	if strings.TrimSpace(options.Validation.SendingAccountID) == "" ||
		strings.TrimSpace(options.Validation.ConfigurationSet) == "" {
		return nil, errors.New("SES feedback validation configuration is required")
	}
	return &Processor{
		repository: options.Repository,
		validation: options.Validation,
	}, nil
}

func (p *Processor) Process(ctx context.Context, body string) error {
	event, err := ParseEvent([]byte(body), p.validation)
	if err != nil {
		return err
	}
	if _, err := p.repository.ApplyFeedback(ctx, event); err != nil {
		return fmt.Errorf("apply SES feedback: %w", err)
	}
	return nil
}
