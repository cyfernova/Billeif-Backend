package handlers

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

func TestPaymentMutationErrorStatus(t *testing.T) {
	tests := []struct {
		err  string
		want int
	}{
		{err: "payment not found", want: http.StatusNotFound},
		{err: "payment reversal reason is required", want: http.StatusBadRequest},
		{err: "posted payments are immutable", want: http.StatusConflict},
		{err: "payment is already reversed", want: http.StatusConflict},
		{err: "database unavailable", want: http.StatusInternalServerError},
	}
	for _, test := range tests {
		if got := paymentMutationErrorStatus(errors.New(test.err)); got != test.want {
			t.Errorf("paymentMutationErrorStatus(%q) = %d, want %d", test.err, got, test.want)
		}
	}
}

func TestPaymentCreateRequiresIdempotencyKeyBeforeServiceCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewPaymentHandler(nil, logger.New())
	router := gin.New()
	router.POST("/payments", func(c *gin.Context) {
		c.Set("business_id", "business-1")
		handler.Create(c)
	})

	request := httptest.NewRequest(http.MethodPost, "/payments", bytes.NewBufferString(`{
		"invoice_id":"01c8f263-40db-4df3-9f54-93a75a95f888",
		"amount":10,
		"payment_method":"cash"
	}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
}
