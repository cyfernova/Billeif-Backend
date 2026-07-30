package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
)

type issueInvoiceRepositoryFake struct {
	*atomicInvoiceRepositoryFake
	lastIssue interfaces.AtomicInvoiceIssue
	result    *interfaces.AtomicInvoiceIssueResult
	issueErr  error
	calls     int
}

func (r *issueInvoiceRepositoryFake) IssueDraftAtomic(ctx context.Context, command interfaces.AtomicInvoiceIssue) (*interfaces.AtomicInvoiceIssueResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.calls++
	r.lastIssue = command
	if r.issueErr != nil {
		return nil, r.issueErr
	}
	return r.result, nil
}

func TestInvoiceServiceIssueBuildsCanonicalTrustedCommand(t *testing.T) {
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	actorID := uuid.NewString()
	renderID := uuid.NewString()
	version := 3
	repo := &issueInvoiceRepositoryFake{
		atomicInvoiceRepositoryFake: &atomicInvoiceRepositoryFake{entries: make(map[string]atomicInvoiceEntry)},
		result: &interfaces.AtomicInvoiceIssueResult{
			Invoice: &models.Invoice{ID: invoiceID, BusinessID: businessID, Version: version + 1, Status: models.InvoiceStatusIssued},
			FinalRender: &models.DocumentRenderJob{
				ID:                   renderID,
				BusinessID:           businessID,
				Kind:                 models.RenderKindFinal,
				SourceInvoiceVersion: &[]int{version + 1}[0],
			},
		},
	}
	service := NewInvoiceService(nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, logger.New())
	ctx := ContextWithActor(context.Background(), ActorContext{
		UserID: actorID, Role: "accountant", RequestID: "request-issue", IPAddress: "127.0.0.1",
	})
	input := IssueInvoiceInput{
		IdempotencyKey:  uuid.NewString(),
		ExpectedVersion: version,
		DocumentType:    "tax_invoice",
		Series:          "INV",
	}

	got, err := service.IssueByBusiness(ctx, businessID, invoiceID, input)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if got.Invoice.ID != invoiceID || got.FinalRender.ID != renderID {
		t.Fatalf("result = %#v, want issued invoice and final render", got)
	}
	command := repo.lastIssue
	if command.BusinessID != businessID || command.InvoiceID != invoiceID || command.ActorID != actorID {
		t.Fatalf("trusted command scope = %#v", command)
	}
	if command.ActorRole != "accountant" || command.RequestID != "request-issue" || command.IPAddress != "127.0.0.1" {
		t.Fatalf("actor metadata = %#v", command)
	}
	if command.RequestHash == "" || command.IdempotencyKey != input.IdempotencyKey ||
		command.ExpectedVersion != version || command.DocumentType != input.DocumentType || command.Series != input.Series {
		t.Fatalf("canonical semantics = %#v", command)
	}
}

func TestInvoiceServiceIssuePublishesOnlyNewCommittedOutboxEvent(t *testing.T) {
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	version := 2
	event := &models.OutboxEvent{ID: uuid.NewString(), BusinessID: businessID}
	finalRender := &models.DocumentRenderJob{ID: uuid.NewString(), Kind: models.RenderKindFinal}
	input := IssueInvoiceInput{
		IdempotencyKey:  uuid.NewString(),
		ExpectedVersion: version,
		DocumentType:    "tax_invoice",
		Series:          "INV",
	}
	ctx := ContextWithActor(context.Background(), ActorContext{UserID: uuid.NewString()})

	t.Run("new issue", func(t *testing.T) {
		repository := &issueInvoiceRepositoryFake{
			atomicInvoiceRepositoryFake: &atomicInvoiceRepositoryFake{entries: make(map[string]atomicInvoiceEntry)},
			result: &interfaces.AtomicInvoiceIssueResult{
				Invoice:     &models.Invoice{ID: invoiceID, BusinessID: businessID, Version: version + 1},
				FinalRender: finalRender,
				OutboxEvent: event,
			},
		}
		publisher := &immediateOutboxPublisherFake{}
		service := NewInvoiceService(nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, logger.New()).
			WithImmediateOutboxPublisher(publisher)

		result, err := service.IssueByBusiness(ctx, businessID, invoiceID, input)

		if err != nil || result == nil || result.Replayed {
			t.Fatalf("new issue result/error = %#v/%v", result, err)
		}
		if publisher.calls != 1 || publisher.event != event || !publisher.hadDeadline {
			t.Fatalf("publisher calls/event/deadline = %d/%#v/%v", publisher.calls, publisher.event, publisher.hadDeadline)
		}
	})

	t.Run("publication failure remains accepted", func(t *testing.T) {
		repository := &issueInvoiceRepositoryFake{
			atomicInvoiceRepositoryFake: &atomicInvoiceRepositoryFake{entries: make(map[string]atomicInvoiceEntry)},
			result: &interfaces.AtomicInvoiceIssueResult{
				Invoice:     &models.Invoice{ID: invoiceID, BusinessID: businessID, Version: version + 1},
				FinalRender: finalRender,
				OutboxEvent: event,
			},
		}
		publisher := &immediateOutboxPublisherFake{err: errors.New("send unavailable")}
		service := NewInvoiceService(nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, logger.New()).
			WithImmediateOutboxPublisher(publisher)

		result, err := service.IssueByBusiness(ctx, businessID, invoiceID, input)

		if err != nil || result == nil {
			t.Fatalf("publication failure result/error = %#v/%v, want accepted issue", result, err)
		}
	})

	t.Run("replay", func(t *testing.T) {
		repository := &issueInvoiceRepositoryFake{
			atomicInvoiceRepositoryFake: &atomicInvoiceRepositoryFake{entries: make(map[string]atomicInvoiceEntry)},
			result: &interfaces.AtomicInvoiceIssueResult{
				Invoice:     &models.Invoice{ID: invoiceID, BusinessID: businessID, Version: version + 1},
				FinalRender: finalRender,
				Replayed:    true,
			},
		}
		publisher := &immediateOutboxPublisherFake{}
		service := NewInvoiceService(nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, logger.New()).
			WithImmediateOutboxPublisher(publisher)

		result, err := service.IssueByBusiness(ctx, businessID, invoiceID, input)

		if err != nil || result == nil || !result.Replayed {
			t.Fatalf("replay result/error = %#v/%v", result, err)
		}
		if publisher.calls != 0 {
			t.Fatalf("replay publisher calls = %d, want 0", publisher.calls)
		}
	})
}

func TestInvoiceServiceIssueValidatesKeyActorAndStableHashedSemantics(t *testing.T) {
	repo := &issueInvoiceRepositoryFake{
		atomicInvoiceRepositoryFake: &atomicInvoiceRepositoryFake{entries: make(map[string]atomicInvoiceEntry)},
		result: &interfaces.AtomicInvoiceIssueResult{
			Invoice:     &models.Invoice{},
			FinalRender: &models.DocumentRenderJob{},
		},
	}
	service := NewInvoiceService(nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, logger.New())
	base := IssueInvoiceInput{
		IdempotencyKey:  uuid.NewString(),
		ExpectedVersion: 1,
		DocumentType:    "tax_invoice",
		Series:          "INV",
	}

	if _, err := service.IssueByBusiness(context.Background(), uuid.NewString(), uuid.NewString(), base); err == nil {
		t.Fatal("issue without actor succeeded")
	}
	actorID := uuid.NewString()
	ctx := ContextWithActor(context.Background(), ActorContext{
		UserID: actorID, Role: "accountant", RequestID: "request-base", IPAddress: "127.0.0.1",
	})
	invalidKey := base
	invalidKey.IdempotencyKey = "not-a-uuid"
	if _, err := service.IssueByBusiness(ctx, uuid.NewString(), uuid.NewString(), invalidKey); err == nil {
		t.Fatal("issue with invalid idempotency key succeeded")
	}

	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	if _, err := service.IssueByBusiness(ctx, businessID, invoiceID, base); err != nil {
		t.Fatalf("base issue: %v", err)
	}
	baseHash := repo.lastIssue.RequestHash
	mutations := []IssueInvoiceInput{
		{IdempotencyKey: base.IdempotencyKey, ExpectedVersion: 2, DocumentType: base.DocumentType, Series: base.Series},
		{IdempotencyKey: base.IdempotencyKey, ExpectedVersion: base.ExpectedVersion, DocumentType: "bill_of_supply", Series: base.Series},
		{IdempotencyKey: base.IdempotencyKey, ExpectedVersion: base.ExpectedVersion, DocumentType: base.DocumentType, Series: "TAX"},
	}
	for _, mutation := range mutations {
		if _, err := service.IssueByBusiness(ctx, businessID, invoiceID, mutation); err != nil {
			t.Fatalf("mutated issue: %v", err)
		}
		if repo.lastIssue.RequestHash == baseHash {
			t.Fatalf("mutation was omitted from canonical hash: %#v", mutation)
		}
	}
	actorMutations := []ActorContext{
		{UserID: actorID, Role: "accountant", RequestID: "request-changed", IPAddress: "127.0.0.1"},
		{UserID: actorID, Role: "accountant", RequestID: "request-base", IPAddress: "127.0.0.2"},
		{UserID: actorID, Role: "admin", RequestID: "request-base", IPAddress: "127.0.0.1"},
	}
	for _, mutation := range actorMutations {
		mutatedContext := ContextWithActor(context.Background(), mutation)
		if _, err := service.IssueByBusiness(mutatedContext, businessID, invoiceID, base); err != nil {
			t.Fatalf("actor-metadata mutation issue: %v", err)
		}
		if repo.lastIssue.RequestHash != baseHash {
			t.Fatalf("transient request metadata changed canonical hash: %#v", mutation)
		}
	}
}

func TestInvoiceServiceIssueCanonicalizesUUIDIdentity(t *testing.T) {
	businessID := uuid.NewString()
	invoiceID := uuid.NewString()
	actorID := uuid.NewString()
	key := uuid.NewString()
	repository := &issueInvoiceRepositoryFake{
		atomicInvoiceRepositoryFake: &atomicInvoiceRepositoryFake{entries: make(map[string]atomicInvoiceEntry)},
		result: &interfaces.AtomicInvoiceIssueResult{
			Invoice:     &models.Invoice{ID: invoiceID, BusinessID: businessID, Version: 2},
			FinalRender: &models.DocumentRenderJob{ID: uuid.NewString(), Kind: models.RenderKindFinal},
		},
	}
	service := NewInvoiceService(nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, logger.New())
	ctx := ContextWithActor(context.Background(), ActorContext{
		UserID: " " + strings.ToUpper(actorID) + " ",
	})

	_, err := service.IssueByBusiness(
		ctx,
		" "+strings.ToUpper(businessID)+" ",
		" "+strings.ToUpper(invoiceID)+" ",
		IssueInvoiceInput{
			IdempotencyKey:  " " + strings.ToUpper(key) + " ",
			ExpectedVersion: 1,
			DocumentType:    "tax_invoice",
			Series:          "INV",
		},
	)

	if err != nil {
		t.Fatalf("canonical issue: %v", err)
	}
	if repository.lastIssue.BusinessID != businessID ||
		repository.lastIssue.InvoiceID != invoiceID ||
		repository.lastIssue.ActorID != actorID ||
		repository.lastIssue.IdempotencyKey != key {
		t.Fatalf("canonical command identity = %#v", repository.lastIssue)
	}
}

func TestInvoiceServiceIssueRejectsIncompleteRepositoryResult(t *testing.T) {
	repo := &issueInvoiceRepositoryFake{
		atomicInvoiceRepositoryFake: &atomicInvoiceRepositoryFake{entries: make(map[string]atomicInvoiceEntry)},
		result:                      &interfaces.AtomicInvoiceIssueResult{Invoice: &models.Invoice{}},
	}
	service := NewInvoiceService(nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, logger.New())
	ctx := ContextWithActor(context.Background(), ActorContext{UserID: uuid.NewString()})
	_, err := service.IssueByBusiness(ctx, uuid.NewString(), uuid.NewString(), IssueInvoiceInput{
		IdempotencyKey: uuid.NewString(), ExpectedVersion: 1, DocumentType: "tax_invoice", Series: "INV",
	})
	if err == nil {
		t.Fatal("incomplete atomic result succeeded")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
}
