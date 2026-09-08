package services

import (
	"context"
	"errors"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/nlp"
)

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
