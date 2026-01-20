package services

import (
	"context"
	"fmt"
	"os"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/nlp"
)

// IntentProcessingService orchestrates natural language intent parsing and product matching
type IntentProcessingService struct {
	intentParser      nlp.IntentParser
	productMatching   *ProductMatchingService
	marketplace       *MarketplaceService
	log               *logger.Logger
}

// ProcessIntentRequest represents a request to process a shopping intent
type ProcessIntentRequest struct {
	NaturalLanguage string
	UserID          string
	MaxResults      int
}

// ProcessIntentResponse represents the response after processing an intent
type ProcessIntentResponse struct {
	ParseResult  *nlp.IntentParseResult
	MatchResults *MatchResults
	Error        string
	Success      bool
}

// NewIntentProcessingService creates a new intent processing service
func NewIntentProcessingService(productMatching *ProductMatchingService, marketplace *MarketplaceService, log *logger.Logger) (*IntentProcessingService, error) {
	// Create intent parser based on configuration
	var parser nlp.IntentParser
	var err error

	provider := os.Getenv("NLP_PROVIDER")
	if provider == "" {
		provider = "claude" // Default to Claude
	}

	switch provider {
	case "claude":
		apiKey := os.Getenv("CLAUDE_API_KEY")
		if apiKey == "" {
			log.Warn("CLAUDE_API_KEY not set, falling back to rule-based parser")
			parser = nlp.NewRuleBasedIntentParser()
		} else {
			config := &nlp.LLMConfig{
				Provider: "claude",
				APIKey:   apiKey,
				Model:    "", // Use default
			}
			parser, err = nlp.NewLLMIntentParser(config)
			if err != nil {
				log.Warn("failed to create Claude parser, falling back to rule-based", "error", err)
				parser = nlp.NewRuleBasedIntentParser()
			}
		}

	case "gemini":
		apiKey := os.Getenv("GEMINI_API_KEY")
		if apiKey == "" {
			log.Warn("GEMINI_API_KEY not set, falling back to rule-based parser")
			parser = nlp.NewRuleBasedIntentParser()
		} else {
			config := &nlp.LLMConfig{
				Provider: "gemini",
				APIKey:   apiKey,
				Model:    "", // Use default
			}
			parser, err = nlp.NewLLMIntentParser(config)
			if err != nil {
				log.Warn("failed to create Gemini parser, falling back to rule-based", "error", err)
				parser = nlp.NewRuleBasedIntentParser()
			}
		}

	default:
		log.Warn("unknown NLP provider, using rule-based parser", "provider", provider)
		parser = nlp.NewRuleBasedIntentParser()
	}

	return &IntentProcessingService{
		intentParser:    parser,
		productMatching: productMatching,
		marketplace:     marketplace,
		log:             log,
	}, nil
}

// ProcessIntent processes a natural language shopping intent
func (s *IntentProcessingService) ProcessIntent(ctx context.Context, req *ProcessIntentRequest) *ProcessIntentResponse {
	response := &ProcessIntentResponse{
		Success: false,
	}

	if req.NaturalLanguage == "" {
		response.Error = "natural language input is required"
		return response
	}

	if req.MaxResults <= 0 {
		req.MaxResults = 20
	}

	// Parse natural language intent
	s.log.Info("parsing natural language intent", "user_id", req.UserID, "intent", req.NaturalLanguage)
	parseResult, err := s.intentParser.ParseIntent(ctx, req.NaturalLanguage)
	if err != nil {
		response.Error = fmt.Sprintf("failed to parse intent: %v", err)
		s.log.Error("intent parsing failed", "error", err, "user_id", req.UserID)
		return response
	}

	response.ParseResult = parseResult

	if !parseResult.Parsed {
		response.Error = fmt.Sprintf("could not parse intent: %s", parseResult.Error)
		return response
	}

	// Validate intent against inventory
	validIntent, err := s.productMatching.ValidateIntentAgainstInventory(ctx, parseResult.Intent)
	if err != nil {
		response.Error = fmt.Sprintf("failed to validate intent: %v", err)
		s.log.Error("inventory validation failed", "error", err, "user_id", req.UserID)
		return response
	}

	if !validIntent {
		// Try to suggest alternatives
		s.log.Warn("no exact match for intent, suggesting alternatives", "user_id", req.UserID)
		alternatives, err := s.productMatching.SuggestAlternativeProducts(ctx, parseResult.Intent, req.MaxResults)
		if err != nil {
			response.Error = "no matching products found in inventory"
			return response
		}

		if len(alternatives) == 0 {
			response.Error = "no matching products found and no alternatives available"
			return response
		}

		response.MatchResults = &MatchResults{
			TotalMatched:  len(alternatives),
			TotalSearched: len(alternatives),
			Products:      alternatives,
			Intent:        parseResult.Intent,
		}
		response.Success = true
		s.log.Info("alternative products suggested", "count", len(alternatives), "user_id", req.UserID)
		return response
	}

	// Find matching products
	s.log.Info("searching for matching products", "user_id", req.UserID, "intent", req.NaturalLanguage)
	matchResults, err := s.productMatching.FindMatchingProducts(ctx, parseResult.Intent, req.MaxResults)
	if err != nil {
		response.Error = fmt.Sprintf("failed to find matching products: %v", err)
		s.log.Error("product matching failed", "error", err, "user_id", req.UserID)
		return response
	}

	response.MatchResults = matchResults
	response.Success = true

	s.log.Info("intent processing completed successfully",
		"user_id", req.UserID,
		"matched_products", len(matchResults.Products),
		"confidence", parseResult.Confidence)

	return response
}

// ValidateConstraints validates that a product meets intent constraints
func (s *IntentProcessingService) ValidateConstraints(product *models.MarketplaceProduct, intent *nlp.ShoppingIntent) *nlp.ConstraintValidator {
	return nlp.NewConstraintValidator(intent)
}

// ApplyConstraints applies intent constraints to filter products
func (s *IntentProcessingService) ApplyConstraints(products []*models.MarketplaceProduct, intent *nlp.ShoppingIntent) []*models.MarketplaceProduct {
	return s.productMatching.FilterByConstraints(products, intent)
}

// RankProducts ranks products by relevance to the intent
func (s *IntentProcessingService) RankProducts(products []*models.MarketplaceProduct, intent *nlp.ShoppingIntent) []*MatchedProduct {
	return s.productMatching.RankProducts(products, intent)
}

// ParseIntentOnly parses a natural language intent without searching for products
func (s *IntentProcessingService) ParseIntentOnly(ctx context.Context, naturalLanguage string) (*nlp.IntentParseResult, error) {
	return s.intentParser.ParseIntent(ctx, naturalLanguage)
}
