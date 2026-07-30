package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
)

type invoiceDeliveryRepositoryFake struct {
	interfaces.CanonicalInvoiceRepository
	command interfaces.AtomicInvoiceDelivery
	result  *interfaces.AtomicInvoiceDeliveryResult
	err     error
	status  *models.EmailDelivery
	getErr  error
	calls   int
}

func (r *invoiceDeliveryRepositoryFake) CreateDeliveryAtomic(
	_ context.Context,
	command interfaces.AtomicInvoiceDelivery,
) (*interfaces.AtomicInvoiceDeliveryResult, error) {
	r.calls++
	r.command = command
	return r.result, r.err
}

func (r *invoiceDeliveryRepositoryFake) GetInvoiceDelivery(
	_ context.Context,
	businessID, invoiceID, deliveryID string,
) (*models.EmailDelivery, error) {
	r.calls++
	if r.getErr != nil {
		return nil, r.getErr
	}
	if r.status == nil || r.status.BusinessID != businessID ||
		r.status.InvoiceID == nil || *r.status.InvoiceID != invoiceID ||
		r.status.ID != deliveryID {
		return nil, interfaces.ErrInvoiceDeliveryNotFound
	}
	return r.status, nil
}

func newInvoiceDeliveryService(repository interfaces.InvoiceDeliveryRepository) *InvoiceService {
	return NewInvoiceService(
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, logger.New(),
		WithInvoiceDeliveryRepository(repository),
	)
}

func TestInvoiceServiceDeliverValidatesUUIDKeyActorAndRecipientBeforeRepositoryAccess(t *testing.T) {
	repository := &invoiceDeliveryRepositoryFake{}
	service := newInvoiceDeliveryService(repository)
	validContext := ContextWithActor(context.Background(), ActorContext{UserID: uuid.NewString()})
	validBusinessID, validInvoiceID, validKey := uuid.NewString(), uuid.NewString(), uuid.NewString()

	tests := []struct {
		name       string
		ctx        context.Context
		businessID string
		invoiceID  string
		input      DeliverInvoiceInput
	}{
		{name: "business", ctx: validContext, businessID: "bad", invoiceID: validInvoiceID, input: DeliverInvoiceInput{IdempotencyKey: validKey, Recipient: "buyer@example.com"}},
		{name: "invoice", ctx: validContext, businessID: validBusinessID, invoiceID: "bad", input: DeliverInvoiceInput{IdempotencyKey: validKey, Recipient: "buyer@example.com"}},
		{name: "key", ctx: validContext, businessID: validBusinessID, invoiceID: validInvoiceID, input: DeliverInvoiceInput{IdempotencyKey: "bad", Recipient: "buyer@example.com"}},
		{name: "actor", ctx: context.Background(), businessID: validBusinessID, invoiceID: validInvoiceID, input: DeliverInvoiceInput{IdempotencyKey: validKey, Recipient: "buyer@example.com"}},
		{name: "recipient", ctx: validContext, businessID: validBusinessID, invoiceID: validInvoiceID, input: DeliverInvoiceInput{IdempotencyKey: validKey, Recipient: "not-an-email"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := service.DeliverByBusiness(test.ctx, test.businessID, test.invoiceID, test.input)
			var invalidKey *idempotency.InvalidKeyError
			var invalidPayload *idempotency.InvalidPayloadError
			if result != nil || (!errors.As(err, &invalidKey) && !errors.As(err, &invalidPayload) &&
				(test.name != "actor" || err == nil)) {
				t.Fatalf("result/error = %#v/%T %v, want validation failure", result, err, err)
			}
		})
	}
	if repository.calls != 0 {
		t.Fatalf("repository calls = %d, want 0", repository.calls)
	}
}

func TestInvoiceServiceDeliverCanonicalizesRecipientAndStableActorHash(t *testing.T) {
	businessID, invoiceID, actorID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	deliveryID, renderJobID := uuid.NewString(), uuid.NewString()
	repository := &invoiceDeliveryRepositoryFake{result: &interfaces.AtomicInvoiceDeliveryResult{
		Delivery: &models.EmailDelivery{
			ID: deliveryID, BusinessID: businessID, InvoiceID: &invoiceID,
			RenderJobID: &renderJobID, Recipient: "buyer@example.com",
			Status: models.EmailDeliveryStatusWaitingForRender,
		},
	}}
	service := newInvoiceDeliveryService(repository)
	baseActor := ActorContext{UserID: actorID, Role: "accountant", RequestID: "request-a", IPAddress: "127.0.0.1"}
	input := DeliverInvoiceInput{IdempotencyKey: uuid.NewString(), Recipient: " Buyer@Example.COM "}

	result, err := service.DeliverByBusiness(
		ContextWithActor(context.Background(), baseActor),
		businessID,
		invoiceID,
		input,
	)
	if err != nil {
		t.Fatalf("DeliverByBusiness() error = %v", err)
	}
	if result.Delivery.ID != deliveryID || result.Delivery.InvoiceID != invoiceID ||
		result.Delivery.RenderJobID != renderJobID || result.Delivery.Recipient != "buyer@example.com" ||
		result.Delivery.Status != models.EmailDeliveryStatusWaitingForRender {
		t.Fatalf("delivery result = %#v", result)
	}
	if repository.command.Recipient != "buyer@example.com" ||
		repository.command.ActorID != actorID ||
		repository.command.ActorRole != baseActor.Role ||
		repository.command.RequestID != baseActor.RequestID ||
		repository.command.IPAddress != baseActor.IPAddress {
		t.Fatalf("atomic delivery command = %#v", repository.command)
	}
	baseHash := repository.command.RequestHash

	changedMetadata := baseActor
	changedMetadata.Role = "admin"
	changedMetadata.RequestID = "request-b"
	changedMetadata.IPAddress = "127.0.0.2"
	_, err = service.DeliverByBusiness(
		ContextWithActor(context.Background(), changedMetadata),
		businessID,
		invoiceID,
		input,
	)
	if err != nil {
		t.Fatalf("delivery with changed audit metadata: %v", err)
	}
	if repository.command.RequestHash != baseHash {
		t.Fatal("transient actor metadata changed canonical request hash")
	}

	_, err = service.DeliverByBusiness(
		ContextWithActor(context.Background(), ActorContext{UserID: uuid.NewString()}),
		businessID,
		invoiceID,
		input,
	)
	if err != nil {
		t.Fatalf("delivery with changed actor: %v", err)
	}
	if repository.command.RequestHash == baseHash {
		t.Fatal("authenticated actor identity was omitted from canonical request hash")
	}
}

func TestInvoiceServiceGetDeliveryStatusReturnsOnlySafeFields(t *testing.T) {
	businessID, invoiceID, deliveryID, renderJobID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	createdAt := time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	sentAt := updatedAt
	repository := &invoiceDeliveryRepositoryFake{status: &models.EmailDelivery{
		ID: deliveryID, BusinessID: businessID, InvoiceID: &invoiceID, RenderJobID: &renderJobID,
		Recipient: "buyer@example.com", Status: models.EmailDeliveryStatusSent,
		ProviderMessageID: "must-not-leak", ErrorMessage: "must-not-leak",
		LeaseOwner: models.StringPointer("must-not-leak"), Attempts: 7,
		Metadata: `{"secret":"must-not-leak"}`, SourceEmail: "private@example.com",
		CreatedAt: createdAt, UpdatedAt: updatedAt, SentAt: &sentAt,
	}}
	service := newInvoiceDeliveryService(repository)

	result, err := service.GetDeliveryStatusByBusiness(
		context.Background(), businessID, invoiceID, deliveryID,
	)
	if err != nil {
		t.Fatalf("GetDeliveryStatusByBusiness() error = %v", err)
	}
	want := InvoiceDeliveryStatus{
		ID: deliveryID, InvoiceID: invoiceID, RenderJobID: renderJobID,
		Recipient: "buyer@example.com", Status: models.EmailDeliveryStatusSent,
		CreatedAt: createdAt, UpdatedAt: updatedAt, SentAt: &sentAt,
	}
	if *result != want {
		t.Fatalf("status = %#v, want %#v", result, want)
	}
}

func TestInvoiceServiceGetDeliveryStatusValidatesTenantInvoiceAndDeliveryUUIDs(t *testing.T) {
	repository := &invoiceDeliveryRepositoryFake{}
	service := newInvoiceDeliveryService(repository)
	valid := uuid.NewString()
	for _, ids := range [][3]string{
		{"bad", valid, valid},
		{valid, "bad", valid},
		{valid, valid, "bad"},
	} {
		result, err := service.GetDeliveryStatusByBusiness(context.Background(), ids[0], ids[1], ids[2])
		var invalid *idempotency.InvalidPayloadError
		if result != nil || !errors.As(err, &invalid) {
			t.Fatalf("result/error = %#v/%T %v, want invalid payload", result, err, err)
		}
	}
	if repository.calls != 0 {
		t.Fatalf("repository calls = %d, want 0", repository.calls)
	}
}

func TestInvoiceServiceDeliveryDTOsFailClosedOnRepositoryIdentityMismatch(t *testing.T) {
	businessID, invoiceID, deliveryID, renderJobID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	base := models.EmailDelivery{
		ID: deliveryID, BusinessID: businessID, InvoiceID: &invoiceID, RenderJobID: &renderJobID,
		Recipient: "buyer@example.com", Status: models.EmailDeliveryStatusQueued,
	}
	mutations := []struct {
		name   string
		mutate func(*models.EmailDelivery)
	}{
		{name: "delivery", mutate: func(delivery *models.EmailDelivery) { delivery.ID = "bad" }},
		{name: "business", mutate: func(delivery *models.EmailDelivery) { delivery.BusinessID = uuid.NewString() }},
		{name: "invoice", mutate: func(delivery *models.EmailDelivery) { delivery.InvoiceID = models.StringPointer(uuid.NewString()) }},
		{name: "render missing", mutate: func(delivery *models.EmailDelivery) { delivery.RenderJobID = nil }},
		{name: "render malformed", mutate: func(delivery *models.EmailDelivery) { delivery.RenderJobID = models.StringPointer("bad") }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			delivery := base
			mutation.mutate(&delivery)
			repository := &invoiceDeliveryRepositoryFake{status: &delivery}
			service := newInvoiceDeliveryService(repository)

			status, err := service.GetDeliveryStatusByBusiness(
				context.Background(), businessID, invoiceID, deliveryID,
			)
			if status != nil || !errors.Is(err, interfaces.ErrInvoiceDeliveryNotFound) {
				t.Fatalf("GET status/error = %#v/%v, want not found", status, err)
			}

			repository.result = &interfaces.AtomicInvoiceDeliveryResult{Delivery: &delivery}
			result, err := service.DeliverByBusiness(
				ContextWithActor(context.Background(), ActorContext{UserID: uuid.NewString()}),
				businessID,
				invoiceID,
				DeliverInvoiceInput{IdempotencyKey: uuid.NewString(), Recipient: "buyer@example.com"},
			)
			if result != nil || err == nil {
				t.Fatalf("POST result/error = %#v/%v, want fail closed", result, err)
			}
		})
	}
}
