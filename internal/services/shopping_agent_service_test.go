package services

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"invoice-backend/pkg/logger"
)

func TestQueryMerchantAgentsA2ASkipsOversizedResponse(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: -1,
			Header:        make(http.Header),
			Body:          io.NopCloser(strings.NewReader(`{"name":"Merchant"}` + strings.Repeat(" ", 1<<20))),
			Request:       req,
		}, nil
	})}
	service := &ShoppingAgentService{agentCardHTTPClient: client, log: logger.New()}

	cards, err := service.QueryMerchantAgentsA2A(context.Background(), []string{"https://seller.example"})
	if err != nil {
		t.Fatalf("expected oversized response to use existing fallback semantics: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("expected oversized response to be skipped, got %d cards", len(cards))
	}
}
