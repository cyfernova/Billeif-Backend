package services

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/nlp"
)

// ProductMatchingService handles product search and matching against intent constraints
type ProductMatchingService struct {
	marketplace *MarketplaceService
	log         *logger.Logger
}

// MatchedProduct represents a product with matching metadata
type MatchedProduct struct {
	Product         *models.MarketplaceProduct
	RelevanceScore  float64
	MatchReasons    []string
	ValidationError string
}

// MatchResults represents results from product matching
type MatchResults struct {
	TotalMatched  int
	TotalSearched int
	Products      []*MatchedProduct
	Intent        *nlp.ShoppingIntent
}

// NewProductMatchingService creates a new product matching service
func NewProductMatchingService(marketplace *MarketplaceService, log *logger.Logger) *ProductMatchingService {
	return &ProductMatchingService{
		marketplace: marketplace,
		log:         log,
	}
}

// FindMatchingProducts finds products matching the given shopping intent
func (s *ProductMatchingService) FindMatchingProducts(ctx context.Context, intent *nlp.ShoppingIntent, limit int) (*MatchResults, error) {
	if intent == nil {
		return nil, fmt.Errorf("shopping intent is required")
	}

	if limit <= 0 {
		limit = 20
	}

	// Build search query from intent
	searchQuery := s.buildSearchQuery(intent)

	// Search products in marketplace
	products, total, err := s.marketplace.SearchProducts(ctx, searchQuery, 0, limit*3) // Get more to filter
	if err != nil {
		return nil, fmt.Errorf("failed to search products: %w", err)
	}

	// Create validator for constraints
	validator := nlp.NewConstraintValidator(intent)

	// Validate and score products
	matched := make([]*MatchedProduct, 0)
	for _, product := range products {
		validationResult := validator.ValidateProduct(product)

		matched = append(matched, &MatchedProduct{
			Product:        product,
			RelevanceScore: validationResult.Score,
			MatchReasons:   validationResult.Reasons,
		})
	}

	// Sort by relevance score (highest first)
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].RelevanceScore > matched[j].RelevanceScore
	})

	// Filter to top N valid matches
	validMatches := make([]*MatchedProduct, 0)
	for _, match := range matched {
		if match.RelevanceScore > 0 {
			validMatches = append(validMatches, match)
			if len(validMatches) >= limit {
				break
			}
		}
	}

	results := &MatchResults{
		TotalMatched:  len(validMatches),
		TotalSearched: len(products),
		Products:      validMatches,
		Intent:        intent,
	}

	s.log.Info("product matching completed",
		"search_query", searchQuery,
		"total_searched", total,
		"matched_count", len(validMatches),
		"intent_categories", fmt.Sprintf("%v", intent.Categories),
		"intent_keywords", fmt.Sprintf("%v", intent.Keywords))

	return results, nil
}

// RankProducts ranks products by relevance to the intent
func (s *ProductMatchingService) RankProducts(products []*models.MarketplaceProduct, intent *nlp.ShoppingIntent) []*MatchedProduct {
	validator := nlp.NewConstraintValidator(intent)

	matched := make([]*MatchedProduct, 0)
	for _, product := range products {
		validationResult := validator.ValidateProduct(product)
		matched = append(matched, &MatchedProduct{
			Product:        product,
			RelevanceScore: validationResult.Score,
			MatchReasons:   validationResult.Reasons,
		})
	}

	// Sort by relevance score (highest first)
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].RelevanceScore > matched[j].RelevanceScore
	})

	return matched
}

// FilterByConstraints filters products that meet all hard constraints
func (s *ProductMatchingService) FilterByConstraints(products []*models.MarketplaceProduct, intent *nlp.ShoppingIntent) []*models.MarketplaceProduct {
	validator := nlp.NewConstraintValidator(intent)
	return validator.FilterValidProducts(products)
}

// buildSearchQuery builds a search query from the shopping intent
func (s *ProductMatchingService) buildSearchQuery(intent *nlp.ShoppingIntent) string {
	var queryParts []string

	// Add keywords
	if len(intent.Keywords) > 0 {
		queryParts = append(queryParts, intent.Keywords...)
	}

	// Add categories if no keywords
	if len(queryParts) == 0 && len(intent.Categories) > 0 {
		queryParts = append(queryParts, intent.Categories...)
	}

	// Build final query
	if len(queryParts) == 0 {
		return "*" // Match all if no search criteria
	}

	return strings.Join(queryParts, " ")
}

// ValidateIntentAgainstInventory checks if products exist for the given intent
func (s *ProductMatchingService) ValidateIntentAgainstInventory(ctx context.Context, intent *nlp.ShoppingIntent) (bool, error) {
	searchQuery := s.buildSearchQuery(intent)

	products, _, err := s.marketplace.SearchProducts(ctx, searchQuery, 0, 1)
	if err != nil {
		return false, err
	}

	return len(products) > 0, nil
}

// SuggestAlternativeProducts suggests similar products if exact match not found
func (s *ProductMatchingService) SuggestAlternativeProducts(ctx context.Context, intent *nlp.ShoppingIntent, limit int) ([]*MatchedProduct, error) {
	if limit <= 0 {
		limit = 5
	}

	// Search with relaxed constraints
	relaxedIntent := &nlp.ShoppingIntent{
		Keywords:   intent.Keywords,
		Categories: intent.Categories,
		// Don't include price range or other hard constraints
	}

	results, err := s.FindMatchingProducts(ctx, relaxedIntent, limit)
	if err != nil {
		return nil, err
	}

	return results.Products, nil
}
