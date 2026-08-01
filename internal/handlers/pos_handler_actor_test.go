package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/middleware"
	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type actorRequiringPOSService struct {
	*services.POSService
	actor services.ActorContext
}

func (s *actorRequiringPOSService) Checkout(
	ctx context.Context,
	businessID, userID, sessionID, idempotencyKey string,
	input services.CheckoutPOSCartInput,
) (*models.Document, error) {
	s.actor = services.ActorFromContext(ctx)
	if s.actor.UserID == "" {
		return nil, errors.New("invoice create actor is required")
	}
	return &models.Document{
		ID:         uuid.NewString(),
		BusinessID: businessID,
		Status:     models.DocumentStatusDraft,
	}, nil
}

func TestPOSCheckoutPropagatesAuthenticatedActorToServiceContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.NewString()
	businessID := uuid.NewString()
	requestID := uuid.NewString()
	service := &actorRequiringPOSService{}
	handler := NewPOSHandler(service, logger.New())
	router := gin.New()
	router.POST("/pos/carts/:id/checkout", func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Set("business_id", businessID)
		c.Set("role", "cashier")
		c.Set(middleware.RequestIDKey, requestID)
		handler.Checkout(c)
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/pos/carts/"+uuid.NewString()+"/checkout",
		bytes.NewBufferString(`{"party_type":"manual","tax_mode":"non_gst"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", uuid.NewString())
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("checkout status = %d, want 201: %s", response.Code, response.Body.String())
	}
	if service.actor.UserID != userID ||
		service.actor.Role != "cashier" ||
		service.actor.RequestID != requestID ||
		service.actor.IPAddress == "" {
		t.Fatalf("checkout actor = %#v", service.actor)
	}
}
