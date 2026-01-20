package nlp

import (
	"encoding/json"
	"fmt"
	"strings"

	"invoice-backend/internal/models"
)

// ConstraintValidator validates products against intent constraints
type ConstraintValidator struct {
	intent *ShoppingIntent
}

// ValidationResult represents the result of constraint validation
type ValidationResult struct {
	Valid    bool
	Score    float64 // 0-100, relevance score
	Reasons  []string
	Warnings []string
}

// NewConstraintValidator creates a new constraint validator
func NewConstraintValidator(intent *ShoppingIntent) *ConstraintValidator {
	return &ConstraintValidator{
		intent: intent,
	}
}

// ValidateProduct validates a single product against the intent constraints
func (cv *ConstraintValidator) ValidateProduct(product *models.MarketplaceProduct) *ValidationResult {
	result := &ValidationResult{
		Valid:   true,
		Score:   100.0,
		Reasons: []string{},
	}

	if cv.intent == nil {
		return result
	}

	// Check price constraint
	if cv.intent.PriceRange != nil {
		if product.Price < cv.intent.PriceRange.Min {
			result.Valid = false
			result.Reasons = append(result.Reasons,
				fmt.Sprintf("price %.2f below minimum %.2f",
					product.Price, cv.intent.PriceRange.Min))
		}
		if product.Price > cv.intent.PriceRange.Max {
			result.Valid = false
			result.Reasons = append(result.Reasons,
				fmt.Sprintf("price %.2f above maximum %.2f",
					product.Price, cv.intent.PriceRange.Max))
		}
	}

	// Check category constraint (categories stored as JSON array)
	if len(cv.intent.Categories) > 0 {
		productCategories := parseCategories(product.Categories)
		categoryMatch := false
		for _, intentCategory := range cv.intent.Categories {
			for _, productCategory := range productCategories {
				if strings.EqualFold(productCategory, intentCategory) {
					categoryMatch = true
					break
				}
			}
			if categoryMatch {
				break
			}
		}
		if !categoryMatch && len(productCategories) > 0 {
			result.Valid = false
			result.Reasons = append(result.Reasons,
				fmt.Sprintf("product categories %v do not match required %v",
					productCategories, cv.intent.Categories))
		}
	}

	// Check availability
	if !product.IsAvailable {
		result.Valid = false
		result.Reasons = append(result.Reasons, "product is not available")
	}

	// Check inventory
	if product.InventoryCount <= 0 {
		result.Valid = false
		result.Reasons = append(result.Reasons, "product out of stock")
	}

	// Calculate relevance score based on matches
	if result.Valid {
		result.Score = calculateRelevanceScore(product, cv.intent)
	} else {
		result.Score = 0.0
	}

	return result
}

// ValidateProducts validates multiple products and returns sorted results
func (cv *ConstraintValidator) ValidateProducts(products []*models.MarketplaceProduct) []*ValidationResult {
	results := make([]*ValidationResult, 0)

	for _, product := range products {
		result := cv.ValidateProduct(product)
		results = append(results, result)
	}

	return results
}

// FilterValidProducts returns only valid products
func (cv *ConstraintValidator) FilterValidProducts(products []*models.MarketplaceProduct) []*models.MarketplaceProduct {
	valid := make([]*models.MarketplaceProduct, 0)

	for _, product := range products {
		if cv.ValidateProduct(product).Valid {
			valid = append(valid, product)
		}
	}

	return valid
}

// Helper functions

func parseCategories(categoriesJSON string) []string {
	var categories []string
	err := json.Unmarshal([]byte(categoriesJSON), &categories)
	if err != nil {
		// If it's not valid JSON, return empty slice
		return []string{}
	}
	return categories
}

func calculateRelevanceScore(product *models.MarketplaceProduct, intent *ShoppingIntent) float64 {
	score := 100.0

	// Reduce score if price is not in ideal range
	if intent.PriceRange != nil {
		midPoint := (intent.PriceRange.Min + intent.PriceRange.Max) / 2
		deviation := abs(product.Price-midPoint) / midPoint
		if deviation > 0.2 {
			score -= deviation * 10 // Deduct up to 10 points for price deviation
		}
	}

	// Increase score for matching keywords in product name or description
	matchedKeywords := 0
	for _, keyword := range intent.Keywords {
		if strings.Contains(strings.ToLower(product.Name), strings.ToLower(keyword)) {
			matchedKeywords++
		}
		if product.Description != nil && strings.Contains(strings.ToLower(*product.Description), strings.ToLower(keyword)) {
			matchedKeywords++
		}
	}
	if len(intent.Keywords) > 0 && matchedKeywords > 0 {
		keywordScore := (float64(matchedKeywords) / float64(len(intent.Keywords)*2)) * 20
		score += keywordScore
	}

	// Bonus for matching categories
	if len(intent.Categories) > 0 {
		productCategories := parseCategories(product.Categories)
		categoryMatches := 0
		for _, intentCategory := range intent.Categories {
			for _, productCategory := range productCategories {
				if strings.EqualFold(productCategory, intentCategory) {
					categoryMatches++
				}
			}
		}
		if categoryMatches > 0 {
			score += 15.0 // Add bonus for category match
		}
	}

	// Ensure score stays within 0-100
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	return score
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
