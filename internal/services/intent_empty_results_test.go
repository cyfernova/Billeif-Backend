package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/nlp"
)

func TestIntentUsesConfiguredLLMForProductAndBudget(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		var request struct {
			Model     string        `json:"model"`
			MaxTokens int           `json:"max_tokens"`
			Messages  []ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Model != "configured-model" || request.MaxTokens != 1024 || len(request.Messages) == 0 {
			t.Errorf("unexpected provider request: %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"keywords":["office","chairs"],"categories":[],"quantity":2,"price_range":{"min":0,"max":5000,"currency":"INR"}}`}}}})
	}))
	defer server.Close()
	log := logger.New()
	llm := NewLLMService(config.LLMConfig{APIURL: server.URL, APIKey: "test-key", Model: "configured-model", Timeout: 5}, log)
	repo := &emptyIntentRepository{}
	marketplace := NewMarketplaceService(repo, log)
	service, err := NewIntentProcessingService(NewProductMatchingService(marketplace, log), marketplace, log, shoppingIntentLLMClient{llm: llm})
	if err != nil {
		t.Fatal(err)
	}
	result := service.ProcessIntent(context.Background(), &ProcessIntentRequest{NaturalLanguage: "Find 2 office chairs under 5000 rupees", UserID: "test-user"})
	if !called || !result.Success || result.ParseResult == nil {
		t.Fatalf("configured model was not used successfully: %#v", result)
	}
	intent := result.ParseResult.Intent
	if intent.Quantity != 2 || intent.PriceRange == nil || intent.PriceRange.Max != 5000 || intent.PriceRange.Currency != "INR" {
		t.Fatalf("lost shopping constraints: %#v", intent)
	}
	if len(repo.queries) == 0 {
		t.Fatal("no product search performed")
	}
	for _, query := range repo.queries {
		if !strings.Contains(query, "office") || !strings.Contains(query, "chairs") {
			t.Fatalf("product description lost: %q", query)
		}
	}
}

type emptyIntentRepository struct {
	interfaces.AP2Repository
	err     error
	queries []string
	pages   []int
}

func (r *emptyIntentRepository) SearchMarketplaceProducts(_ context.Context, query string, page, _ int) ([]*models.MarketplaceProduct, int64, error) {
	r.queries = append(r.queries, query)
	r.pages = append(r.pages, page)
	return []*models.MarketplaceProduct{}, 0, r.err
}

func TestIntentEmptyMarketplaceIsSuccessfulSearch(t *testing.T) {
	repo := &emptyIntentRepository{}
	log := logger.New()
	matching := NewProductMatchingService(NewMarketplaceService(repo, log), log)
	svc := &IntentProcessingService{intentParser: nlp.NewRuleBasedIntentParser(), productMatching: matching, log: log}
	result := svc.ProcessIntent(context.Background(), &ProcessIntentRequest{NaturalLanguage: "Find 2 office chairs", UserID: "test-user", MaxResults: 20})
	if !result.Success || result.MatchResults == nil || result.MatchResults.TotalMatched != 0 || result.MatchResults.Products == nil {
		t.Fatalf("expected successful empty result, got %#v", result)
	}
	for i, query := range repo.queries {
		if query == "*" || repo.pages[i] != 1 {
			t.Fatalf("invalid repository search: query=%q page=%d", query, repo.pages[i])
		}
	}
}

func TestIntentSearchFailureRemainsFailure(t *testing.T) {
	repo := &emptyIntentRepository{err: errors.New("search unavailable")}
	log := logger.New()
	matching := NewProductMatchingService(NewMarketplaceService(repo, log), log)
	svc := &IntentProcessingService{intentParser: nlp.NewRuleBasedIntentParser(), productMatching: matching, log: log}
	result := svc.ProcessIntent(context.Background(), &ProcessIntentRequest{NaturalLanguage: "Find 2 office chairs", UserID: "test-user", MaxResults: 20})
	if result.Success || result.Error == "" {
		t.Fatal("repository failures must not become empty successful searches")
	}
}
