package services

import (
	"context"
	"errors"
	"testing"

	"invoice-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type invoiceActorRepositoryStub struct {
	bySubject func(string) (*models.User, error)
	byID      func(string) (*models.User, error)
}

func (r invoiceActorRepositoryStub) GetByCognitoID(_ context.Context, subject string) (*models.User, error) {
	return r.bySubject(subject)
}
func (r invoiceActorRepositoryStub) GetByID(_ context.Context, id string) (*models.User, error) {
	return r.byID(id)
}

func TestInvoiceActorResolvesAuditForeignKeyWithoutChangingAuthorization(t *testing.T) {
	subject, databaseID := uuid.NewString(), uuid.NewString()
	ctx := ContextWithActor(context.Background(), ActorContext{UserID: subject, Role: "viewer", RequestID: "request"})
	service := &InvoiceService{actorUsers: invoiceActorRepositoryStub{
		bySubject: func(got string) (*models.User, error) {
			if got != subject {
				t.Fatalf("subject = %q", got)
			}
			return &models.User{ID: databaseID}, nil
		},
		byID: func(string) (*models.User, error) { t.Fatal("must prefer verified subject mapping"); return nil, nil },
	}}
	actor, err := service.invoiceActor(ctx, "create")
	if err != nil {
		t.Fatal(err)
	}
	if actor.UserID != databaseID || actor.Role != "viewer" || actor.RequestID != "request" {
		t.Fatalf("incorrect audit actor: %#v", actor)
	}
	if ActorFromContext(ctx).UserID != subject {
		t.Fatal("authorization identity was changed")
	}
}

func TestInvoiceActorDoesNotFallbackOnLookupFailure(t *testing.T) {
	outage := errors.New("database unavailable")
	service := &InvoiceService{actorUsers: invoiceActorRepositoryStub{
		bySubject: func(string) (*models.User, error) { return nil, outage },
		byID: func(string) (*models.User, error) {
			t.Fatal("lookup failure must not become another identity")
			return nil, nil
		},
	}}
	_, err := service.invoiceActor(ContextWithActor(context.Background(), ActorContext{UserID: uuid.NewString()}), "create")
	if !errors.Is(err, outage) {
		t.Fatalf("expected lookup error, got %v", err)
	}
}

func TestInvoiceActorAcceptsExistingDatabaseIdentityAndRejectsMissingUser(t *testing.T) {
	id := uuid.NewString()
	service := &InvoiceService{actorUsers: invoiceActorRepositoryStub{
		bySubject: func(string) (*models.User, error) { return nil, gorm.ErrRecordNotFound },
		byID:      func(got string) (*models.User, error) { return &models.User{ID: got}, nil },
	}}
	actor, err := service.invoiceActor(ContextWithActor(context.Background(), ActorContext{UserID: id}), "create")
	if err != nil || actor.UserID != id {
		t.Fatalf("actor=%#v err=%v", actor, err)
	}
	service.actorUsers = invoiceActorRepositoryStub{
		bySubject: func(string) (*models.User, error) { return nil, gorm.ErrRecordNotFound },
		byID:      func(string) (*models.User, error) { return nil, gorm.ErrRecordNotFound },
	}
	if _, err := service.invoiceActor(ContextWithActor(context.Background(), ActorContext{UserID: id}), "create"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing user must fail: %v", err)
	}
}
