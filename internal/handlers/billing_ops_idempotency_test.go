package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestBillingOpsGenerateInvoiceSubscriptionNowRequiresIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewBillingOpsHandler(nil, logger.New())
	router := gin.New()
	router.POST("/invoice-subscriptions/:id/generate-now", func(ctx *gin.Context) {
		ctx.Set("business_id", uuid.NewString())
		handler.GenerateInvoiceSubscriptionNow(ctx)
	})
	request := httptest.NewRequest(http.MethodPost, "/invoice-subscriptions/"+uuid.NewString()+"/generate-now", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key status = %d, want 400: %s", response.Code, response.Body.String())
	}
}
