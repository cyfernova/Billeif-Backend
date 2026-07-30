package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
)

type previewInvoiceRepositoryFake struct {
	interfaces.CanonicalInvoiceRepository
	calls   int
	command interfaces.AtomicInvoicePreview
	result  *interfaces.AtomicInvoicePreviewResult
	err     error
}

func (r *previewInvoiceRepositoryFake) RequestPreviewAtomic(
	_ context.Context,
	command interfaces.AtomicInvoicePreview,
) (*interfaces.AtomicInvoicePreviewResult, error) {
	r.calls++
	r.command = command
	return r.result, r.err
}

type immediateOutboxPublisherFake struct {
	calls       int
	event       *models.OutboxEvent
	err         error
	hadDeadline bool
}

func (p *immediateOutboxPublisherFake) TryPublish(ctx context.Context, event *models.OutboxEvent) error {
	p.calls++
	p.event = event
	_, p.hadDeadline = ctx.Deadline()
	return p.err
}

func TestInvoiceServicePreviewValidatesTrustedIdentityBeforeRepositoryAccess(t *testing.T) {
	repository := &previewInvoiceRepositoryFake{}
	publisher := &immediateOutboxPublisherFake{}
	service := NewInvoiceService(nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, logger.New()).
		WithImmediateOutboxPublisher(publisher)
	validContext := ContextWithActor(context.Background(), ActorContext{UserID: uuid.NewString()})

	fixtures := []struct {
		name      string
		ctx       context.Context
		invoiceID string
		key       string
		assert    func(error) bool
	}{
		{
			name: "invalid key", ctx: validContext, invoiceID: uuid.NewString(), key: "not-a-uuid",
			assert: func(err error) bool {
				var invalid *idempotency.InvalidKeyError
				return errors.As(err, &invalid)
			},
		},
		{
			name: "invalid path invoice", ctx: validContext, invoiceID: "not-a-uuid", key: uuid.NewString(),
			assert: func(err error) bool {
				var invalid *idempotency.InvalidPayloadError
				return errors.As(err, &invalid)
			},
		},
		{
			name: "invalid business", ctx: validContext, invoiceID: uuid.NewString(), key: uuid.NewString(),
			assert: func(err error) bool {
				var invalid *idempotency.InvalidPayloadError
				return errors.As(err, &invalid)
			},
		},
		{
			name: "missing actor", ctx: context.Background(), invoiceID: uuid.NewString(), key: uuid.NewString(),
			assert: func(err error) bool {
				return err != nil && err.Error() == "invoice preview actor is required"
			},
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			businessID := uuid.NewString()
			if fixture.name == "invalid business" {
				businessID = "not-a-uuid"
			}
			result, err := service.PreviewByBusiness(
				fixture.ctx,
				businessID,
				fixture.invoiceID,
				PreviewInvoiceInput{IdempotencyKey: fixture.key},
			)

			if result != nil || err == nil || !fixture.assert(err) {
				t.Fatalf("result/error = %#v/%T %v", result, err, err)
			}
			if repository.calls != 0 || publisher.calls != 0 {
				t.Fatalf("repository/publisher calls = %d/%d, want 0/0", repository.calls, publisher.calls)
			}
		})
	}
}

func TestInvoiceServicePreviewCanonicalizesUUIDIdentityWhitespace(t *testing.T) {
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	idempotencyKey := uuid.NewString()
	actorID := uuid.NewString()
	repository := &previewInvoiceRepositoryFake{
		result: &interfaces.AtomicInvoicePreviewResult{
			RenderJob: &models.DocumentRenderJob{ID: uuid.NewString()},
			Replayed:  true,
		},
	}
	service := NewInvoiceService(nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, logger.New())
	ctx := ContextWithActor(context.Background(), ActorContext{UserID: strings.ToUpper(actorID)})

	if _, err := service.PreviewByBusiness(
		ctx,
		" "+strings.ToUpper(businessID)+" ",
		" "+strings.ToUpper(invoiceID)+" ",
		PreviewInvoiceInput{IdempotencyKey: " " + strings.ToUpper(idempotencyKey) + " "},
	); err != nil {
		t.Fatalf("whitespace preview: %v", err)
	}
	whitespaceHash := repository.command.RequestHash
	if repository.command.BusinessID != businessID || repository.command.InvoiceID != invoiceID ||
		repository.command.IdempotencyKey != idempotencyKey || repository.command.ActorID != actorID {
		t.Fatalf("canonical command = %#v", repository.command)
	}
	if _, err := service.PreviewByBusiness(
		ctx,
		businessID,
		invoiceID,
		PreviewInvoiceInput{IdempotencyKey: idempotencyKey},
	); err != nil {
		t.Fatalf("canonical preview: %v", err)
	}
	if repository.command.RequestHash != whitespaceHash {
		t.Fatalf("equivalent UUID whitespace changed hash: %s != %s", repository.command.RequestHash, whitespaceHash)
	}
}

func TestInvoiceServicePreviewHashesStableActorIdentityAndBuildsAuditCommand(t *testing.T) {
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	actorID := uuid.NewString()
	job := &models.DocumentRenderJob{ID: uuid.NewString(), BusinessID: businessID, Kind: models.RenderKindPreview}
	repository := &previewInvoiceRepositoryFake{
		result: &interfaces.AtomicInvoicePreviewResult{RenderJob: job, Replayed: true},
	}
	service := NewInvoiceService(nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, logger.New())
	input := PreviewInvoiceInput{IdempotencyKey: uuid.NewString()}
	baseActor := ActorContext{
		UserID: actorID, Role: "accountant", RequestID: "request-one", IPAddress: "127.0.0.1",
	}

	if _, err := service.PreviewByBusiness(ContextWithActor(context.Background(), baseActor), businessID, invoiceID, input); err != nil {
		t.Fatalf("base preview: %v", err)
	}
	baseHash := repository.command.RequestHash
	if repository.command.BusinessID != businessID || repository.command.InvoiceID != invoiceID ||
		repository.command.ActorID != actorID || repository.command.ActorRole != baseActor.Role ||
		repository.command.RequestID != baseActor.RequestID || repository.command.IPAddress != baseActor.IPAddress ||
		repository.command.Command != "invoice.preview" || repository.command.IdempotencyKey != input.IdempotencyKey ||
		baseHash == "" {
		t.Fatalf("trusted preview command = %#v", repository.command)
	}

	metadataMutation := ActorContext{
		UserID: actorID, Role: "admin", RequestID: "request-two", IPAddress: "127.0.0.2",
	}
	if _, err := service.PreviewByBusiness(
		ContextWithActor(context.Background(), metadataMutation), businessID, invoiceID, input,
	); err != nil {
		t.Fatalf("metadata mutation preview: %v", err)
	}
	if repository.command.RequestHash != baseHash {
		t.Fatalf("transient actor metadata changed stable hash: %s != %s", repository.command.RequestHash, baseHash)
	}
	if repository.command.ActorRole != metadataMutation.Role ||
		repository.command.RequestID != metadataMutation.RequestID ||
		repository.command.IPAddress != metadataMutation.IPAddress {
		t.Fatalf("audit metadata was not refreshed: %#v", repository.command)
	}

	differentActor := metadataMutation
	differentActor.UserID = uuid.NewString()
	if _, err := service.PreviewByBusiness(
		ContextWithActor(context.Background(), differentActor), businessID, invoiceID, input,
	); err != nil {
		t.Fatalf("actor mutation preview: %v", err)
	}
	if repository.command.RequestHash == baseHash {
		t.Fatal("authenticated actor identity was omitted from canonical hash")
	}
}

func TestInvoiceServicePreviewPublishesOnlyNewOutboxWithBoundedContext(t *testing.T) {
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	event := &models.OutboxEvent{ID: uuid.NewString(), BusinessID: businessID, Payload: `{"type":"generate_document_pdf"}`}
	job := &models.DocumentRenderJob{ID: uuid.NewString(), BusinessID: businessID, Kind: models.RenderKindPreview}

	t.Run("new request", func(t *testing.T) {
		repository := &previewInvoiceRepositoryFake{
			result: &interfaces.AtomicInvoicePreviewResult{RenderJob: job, OutboxEvent: event},
		}
		publisher := &immediateOutboxPublisherFake{}
		service := NewInvoiceService(nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, logger.New()).
			WithImmediateOutboxPublisher(publisher)

		result, err := service.PreviewByBusiness(
			ContextWithActor(context.Background(), ActorContext{UserID: uuid.NewString()}),
			businessID,
			invoiceID,
			PreviewInvoiceInput{IdempotencyKey: uuid.NewString()},
		)

		if err != nil || result == nil || result.RenderJob != job || result.Replayed {
			t.Fatalf("result/error = %#v/%v", result, err)
		}
		if publisher.calls != 1 || publisher.event != event || !publisher.hadDeadline {
			t.Fatalf("publisher calls/event/deadline = %d/%#v/%v", publisher.calls, publisher.event, publisher.hadDeadline)
		}
	})

	t.Run("publisher failure remains accepted", func(t *testing.T) {
		repository := &previewInvoiceRepositoryFake{
			result: &interfaces.AtomicInvoicePreviewResult{RenderJob: job, OutboxEvent: event},
		}
		publisher := &immediateOutboxPublisherFake{err: errors.New("sqs unavailable")}
		service := NewInvoiceService(nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, logger.New()).
			WithImmediateOutboxPublisher(publisher)

		result, err := service.PreviewByBusiness(
			ContextWithActor(context.Background(), ActorContext{UserID: uuid.NewString()}),
			businessID,
			invoiceID,
			PreviewInvoiceInput{IdempotencyKey: uuid.NewString()},
		)

		if err != nil || result == nil || result.RenderJob != job {
			t.Fatalf("publisher failure result/error = %#v/%v, want accepted job", result, err)
		}
	})

	t.Run("replay", func(t *testing.T) {
		repository := &previewInvoiceRepositoryFake{
			result: &interfaces.AtomicInvoicePreviewResult{RenderJob: job, Replayed: true},
		}
		publisher := &immediateOutboxPublisherFake{}
		service := NewInvoiceService(nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, logger.New()).
			WithImmediateOutboxPublisher(publisher)

		result, err := service.PreviewByBusiness(
			ContextWithActor(context.Background(), ActorContext{UserID: uuid.NewString()}),
			businessID,
			invoiceID,
			PreviewInvoiceInput{IdempotencyKey: uuid.NewString()},
		)

		if err != nil || result == nil || !result.Replayed {
			t.Fatalf("replay result/error = %#v/%v", result, err)
		}
		if publisher.calls != 0 {
			t.Fatalf("publisher calls = %d, want 0 for replay", publisher.calls)
		}
	})
}

func TestInvoiceServicePreviewPublisherDeadlineIsShort(t *testing.T) {
	businessID := uuid.NewString()
	event := &models.OutboxEvent{ID: uuid.NewString()}
	repository := &previewInvoiceRepositoryFake{
		result: &interfaces.AtomicInvoicePreviewResult{
			RenderJob:   &models.DocumentRenderJob{ID: uuid.NewString()},
			OutboxEvent: event,
		},
	}
	publisher := &deadlineMeasuringPublisher{}
	service := NewInvoiceService(nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, logger.New()).
		WithImmediateOutboxPublisher(publisher)

	_, err := service.PreviewByBusiness(
		ContextWithActor(context.Background(), ActorContext{UserID: uuid.NewString()}),
		businessID,
		uuid.NewString(),
		PreviewInvoiceInput{IdempotencyKey: uuid.NewString()},
	)

	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if publisher.remaining <= 0 || publisher.remaining > 3*time.Second {
		t.Fatalf("publisher deadline remaining = %v, want short positive deadline", publisher.remaining)
	}
}

type deadlineMeasuringPublisher struct {
	remaining time.Duration
}

func (p *deadlineMeasuringPublisher) TryPublish(ctx context.Context, _ *models.OutboxEvent) error {
	deadline, ok := ctx.Deadline()
	if ok {
		p.remaining = time.Until(deadline)
	}
	return nil
}
