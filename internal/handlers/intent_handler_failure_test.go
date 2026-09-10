package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type unavailableIntentClient struct{}

func (unavailableIntentClient) Call(context.Context, string) (string, error) {
	return "", errors.New("provider unavailable")
}

func TestIntentEndpointsHandleProviderFailure(t *testing.T) {
	log := logger.New()
	service, err := services.NewIntentProcessingService(nil, nil, log, unavailableIntentClient{})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewIntentHandler(service, log)
	for _, tc := range []struct {
		name   string
		handle gin.HandlerFunc
		status int
	}{
		{"process", handler.ProcessIntent, http.StatusBadRequest},
		{"validate", handler.ValidateIntent, http.StatusBadRequest},
		{"parse", handler.ParseIntent, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.POST("/", tc.handle)
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"intent":"Find chairs"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status || !strings.Contains(response.Body.String(), "error") {
				t.Fatalf("unexpected failure response: %d %s", response.Code, response.Body.String())
			}
		})
	}
}
