package nlp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ShoppingIntent represents a parsed shopping intent with extracted constraints
type ShoppingIntent struct {
	Categories      []string          `json:"categories"`
	Keywords        []string          `json:"keywords"`
	PriceRange      *PriceRange       `json:"price_range,omitempty"`
	Brands          []string          `json:"brands,omitempty"`
	Specifications  map[string]string `json:"specifications,omitempty"`
	DeliveryDate    *time.Time        `json:"delivery_date,omitempty"`
	MaxDeliveryDays *int              `json:"max_delivery_days,omitempty"`
	Urgency         string            `json:"urgency,omitempty"` // low, medium, high, urgent
	Quantity        int               `json:"quantity,omitempty"`
	RawIntent       string            `json:"raw_intent"` // Original user input
}

// PriceRange represents min/max price constraints
type PriceRange struct {
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Currency string  `json:"currency"`
}

// IntentParseResult represents the result of parsing an intent
type IntentParseResult struct {
	Intent      *ShoppingIntent `json:"intent"`
	Confidence  float64         `json:"confidence"` // 0-100, confidence score
	RawResponse string          `json:"raw_response"`
	Error       string          `json:"error,omitempty"`
	Parsed      bool            `json:"parsed"`
}

// IntentParser interface for different NLP implementations
type IntentParser interface {
	ParseIntent(ctx context.Context, naturalLanguage string) (*IntentParseResult, error)
}

// LLMConfig contains configuration for LLM-based intent parsing
type LLMConfig struct {
	Provider string // "claude", "gemini", "openai"
	APIKey   string
	Model    string
	Timeout  time.Duration
}

// LLMIntentParser uses an LLM to parse natural language intents
type LLMIntentParser struct {
	provider string
	apiKey   string
	model    string
	timeout  time.Duration
	client   LLMClient
}

// LLMClient interface for interacting with LLM services
type LLMClient interface {
	Call(ctx context.Context, prompt string) (string, error)
}

// NewIntentParserWithClient reuses an application's configured provider client.
func NewIntentParserWithClient(client LLMClient) *LLMIntentParser {
	return &LLMIntentParser{client: client, timeout: 10 * time.Second}
}

// NewLLMIntentParser creates a new LLM-based intent parser
func NewLLMIntentParser(config *LLMConfig) (*LLMIntentParser, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("API key required for LLM parser")
	}

	var client LLMClient
	var err error

	switch config.Provider {
	case "claude":
		client, err = NewClaudeClient(config.APIKey, config.Model)
	case "gemini":
		client, err = NewGeminiClient(config.APIKey, config.Model)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", config.Provider)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create LLM client: %w", err)
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	return &LLMIntentParser{
		provider: config.Provider,
		apiKey:   config.APIKey,
		model:    config.Model,
		timeout:  timeout,
		client:   client,
	}, nil
}

// ParseIntent parses a natural language shopping intent using Claude
func (p *LLMIntentParser) ParseIntent(ctx context.Context, naturalLanguage string) (*IntentParseResult, error) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	// Build the prompt for intent extraction
	prompt := buildIntentExtractionPrompt(naturalLanguage)

	// Call the LLM
	response, err := p.client.Call(ctx, prompt)
	if err != nil {
		return &IntentParseResult{
			Parsed: false,
			Error:  fmt.Sprintf("LLM call failed: %v", err),
		}, err
	}

	// Parse the JSON response
	result := &IntentParseResult{
		RawResponse: response,
	}

	intent := &ShoppingIntent{}
	if err := json.Unmarshal([]byte(response), intent); err != nil {
		result.Error = fmt.Sprintf("Failed to parse LLM response: %v", err)
		result.Parsed = false
		return result, nil // Return nil error since parsing failed gracefully
	}
	if strings.TrimSpace(strings.Join(intent.Keywords, " ")+strings.Join(intent.Categories, " ")) == "" {
		result.Error = "Describe the product you want to find."
		return result, nil
	}
	if intent.Quantity < 0 || (intent.PriceRange != nil && (math.IsNaN(intent.PriceRange.Min) || math.IsInf(intent.PriceRange.Min, 0) || math.IsNaN(intent.PriceRange.Max) || math.IsInf(intent.PriceRange.Max, 0) || intent.PriceRange.Min < 0 || intent.PriceRange.Max <= 0 || intent.PriceRange.Min > intent.PriceRange.Max)) {
		result.Error = "The shopping quantity or budget could not be understood."
		return result, nil
	}
	if intent.Quantity == 0 {
		intent.Quantity = 1
	}

	intent.RawIntent = naturalLanguage
	result.Intent = intent
	result.Parsed = true
	result.Confidence = extractConfidence(response)

	return result, nil
}

// buildIntentExtractionPrompt creates a prompt for extracting shopping intents
func buildIntentExtractionPrompt(userIntent string) string {
	return fmt.Sprintf(`You are a shopping assistant that extracts structured intent from natural language queries.
Parse the following shopping request and extract the intent as JSON.

User Query: "%s"

Extract and return ONLY a valid JSON object (no markdown, no code blocks, just raw JSON) with the following structure:
{
  "categories": ["category1", "category2"],
  "keywords": ["keyword1", "keyword2"],
  "price_range": {
    "min": 100,
    "max": 5000,
    "currency": "INR"
  },
  "brands": ["brand1", "brand2"],
  "specifications": {
    "color": "blue",
    "size": "large"
  },
  "max_delivery_days": 7,
  "urgency": "medium",
  "quantity": 1
}

Rules:
- Return ONLY valid JSON, no explanations
- Include only fields that are explicitly mentioned or strongly implied
- For price ranges, infer reasonable defaults if not specified (omit if not mentioned)
- Categories should be general product categories (electronics, clothing, etc.)
- Keywords should be product-specific search terms
- Urgency: "low" for flexible, "medium" for normal, "high" for soon, "urgent" for ASAP
- Default quantity to 1 if not mentioned
- Return empty arrays for categories/keywords if not determinable, but omit other fields if not mentioned
- Ensure all numeric values are numbers, not strings`, userIntent)
}

// extractConfidence attempts to extract a confidence score from the LLM response
func extractConfidence(response string) float64 {
	// Simple heuristic: if response is valid JSON, confidence is high
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(response), &data); err == nil {
		// Valid JSON structure
		return 85.0
	}
	return 60.0
}

// RuleBasedIntentParser is a simple rule-based intent parser for fallback
type RuleBasedIntentParser struct {
	patterns map[string][]string // pattern -> categories
}

// NewRuleBasedIntentParser creates a new rule-based intent parser
func NewRuleBasedIntentParser() *RuleBasedIntentParser {
	return &RuleBasedIntentParser{
		patterns: map[string][]string{
			"phone":      {"electronics", "smartphones"},
			"laptop":     {"electronics", "computers"},
			"shirt":      {"clothing", "apparel"},
			"shoes":      {"footwear", "shoes"},
			"book":       {"books", "media"},
			"watch":      {"electronics", "accessories"},
			"headphones": {"electronics", "audio"},
		},
	}
}

// ParseIntent parses intent using simple rule-based matching
func (p *RuleBasedIntentParser) ParseIntent(ctx context.Context, naturalLanguage string) (*IntentParseResult, error) {
	intent := &ShoppingIntent{
		RawIntent: naturalLanguage,
		Keywords:  extractKeywords(naturalLanguage),
		Quantity:  extractQuantity(naturalLanguage),
	}

	// Extract price range if mentioned
	if priceRange := extractPriceRange(naturalLanguage); priceRange != nil {
		intent.PriceRange = priceRange
	}

	// Extract urgency
	if isUrgent(naturalLanguage) {
		intent.Urgency = "urgent"
	} else if isSoon(naturalLanguage) {
		intent.Urgency = "high"
	} else {
		intent.Urgency = "medium"
	}

	return &IntentParseResult{
		Intent:     intent,
		Parsed:     true,
		Confidence: 60.0, // Lower confidence for rule-based
	}, nil
}

// Helper functions for rule-based parsing

var shoppingQuantityPattern = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:find|buy|get|order|need|i need|i want|show me)\s+([1-9][0-9]*)\s+([a-z]+)`)

func extractQuantity(text string) int {
	match := shoppingQuantityPattern.FindStringSubmatch(text)
	if len(match) != 3 {
		return 1
	}
	switch strings.ToLower(match[2]) {
	case "rupees", "rs", "inr", "dollars", "usd", "euros", "eur":
		return 1
	}
	quantity, err := strconv.Atoi(match[1])
	if err != nil || quantity <= 0 {
		return 1
	}
	return quantity
}

func extractKeywords(text string) []string {
	// Simple keyword extraction (in production, use more sophisticated NLP)
	keywords := []string{}
	// This is a placeholder - in production, use proper tokenization and filtering
	return keywords
}

func extractPriceRange(text string) *PriceRange {
	// Simple price range extraction
	// In production, use regex patterns for various formats
	// e.g., "under 5000", "between 1000 and 5000", "max 10000"
	return nil // Placeholder
}

func isUrgent(text string) bool {
	urgentKeywords := []string{"urgent", "asap", "immediately", "today", "right now"}
	for _, kw := range urgentKeywords {
		if contains(text, kw) {
			return true
		}
	}
	return false
}

func isSoon(text string) bool {
	soonKeywords := []string{"tomorrow", "this week", "asap", "soon", "quick"}
	for _, kw := range soonKeywords {
		if contains(text, kw) {
			return true
		}
	}
	return false
}

func contains(text, substring string) bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r < 'a' || r > 'z'
	})
	return strings.Contains(" "+strings.Join(words, " ")+" ", " "+strings.ToLower(substring)+" ")
}
