package services

import (
	"context"
	"fmt"
	"invoice-backend/internal/models"
	"invoice-backend/pkg/ap2"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type shoppingIdentityRepo struct {
	*cartSecurityRepository
	expectedUser string
}

func (r *shoppingIdentityRepo) CreateCartMandate(_ context.Context, cart *models.CartMandate) error {
	if cart.UserID != r.expectedUser {
		return fmt.Errorf("cart_mandates_user_id_fkey")
	}
	clone := *cart
	r.cart = &clone
	return nil
}

func TestShoppingCartResolvesDatabaseUserAndPreservesScope(t *testing.T) {
	subject, databaseID, businessID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	repo := &shoppingIdentityRepo{cartSecurityRepository: &cartSecurityRepository{}, expectedUser: databaseID}
	signer, err := ap2.NewSignatureService()
	require.NoError(t, err)
	svc := (&ShoppingAgentService{ap2Repo: repo, signer: signer, mandateSvc: ap2.NewMandateService(nil, nil)}).WithUserRepository(invoiceActorRepositoryStub{
		bySubject: func(value string) (*models.User, error) {
			if value == subject {
				return &models.User{ID: databaseID}, nil
			}
			return nil, gorm.ErrRecordNotFound
		},
		byID: func(value string) (*models.User, error) {
			if value == databaseID {
				return &models.User{ID: databaseID}, nil
			}
			return nil, gorm.ErrRecordNotFound
		},
	})
	request := &CreateCartMandateRequest{UserID: subject, BusinessID: businessID, ShoppingAgentID: uuid.NewString()}
	cart, err := svc.CreateCartMandate(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, databaseID, cart.UserID)
	require.Equal(t, subject, request.UserID)
	loaded, err := svc.GetCartMandateForScope(context.Background(), cart.ID, subject, businessID)
	require.NoError(t, err)
	require.Equal(t, cart.ID, loaded.ID)
	_, err = svc.GetCartMandateForScope(context.Background(), cart.ID, subject, uuid.NewString())
	require.ErrorIs(t, err, ErrCartMandateNotFound)
	_, err = svc.GetCartMandateForScope(context.Background(), cart.ID, uuid.NewString(), businessID)
	require.ErrorIs(t, err, ErrCartMandateNotFound)
}
