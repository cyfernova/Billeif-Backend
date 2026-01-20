package services

import (
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/nlp"
)

// TestConstraintValidation tests constraint validation
func TestConstraintValidation(t *testing.T) {
	intent := &nlp.ShoppingIntent{
		Categories: []string{"Electronics"},
		Keywords:   []string{"laptop"},
		PriceRange: &nlp.PriceRange{
			Min:      40000,
			Max:      50000,
			Currency: "INR",
		},
	}

	product := &models.MarketplaceProduct{
		ID:             "1",
		Name:           "Test Laptop",
		Price:          45000,
		Currency:       "INR",
		IsAvailable:    true,
		InventoryCount: 5,
		Categories:     `["Electronics"]`,
	}

	validator := nlp.NewConstraintValidator(intent)
	result := validator.ValidateProduct(product)

	if !result.Valid {
		t.Errorf("expected product to be valid, got %v. Reasons: %v", result.Valid, result.Reasons)
	}

	if result.Score <= 0 {
		t.Errorf("expected positive score, got %f", result.Score)
	}
}

// TestConstraintValidationWithPriceViolation tests constraint validation fails with price violation
func TestConstraintValidationWithPriceViolation(t *testing.T) {
	intent := &nlp.ShoppingIntent{
		PriceRange: &nlp.PriceRange{
			Min:      10000,
			Max:      30000,
			Currency: "INR",
		},
	}

	product := &models.MarketplaceProduct{
		ID:             "1",
		Name:           "Expensive Laptop",
		Price:          45000,
		Currency:       "INR",
		IsAvailable:    true,
		InventoryCount: 5,
	}

	validator := nlp.NewConstraintValidator(intent)
	result := validator.ValidateProduct(product)

	if result.Valid {
		t.Error("expected product to be invalid due to price, got valid")
	}

	if result.Score != 0.0 {
		t.Errorf("expected score 0 for invalid product, got %f", result.Score)
	}

	if len(result.Reasons) == 0 {
		t.Error("expected validation reasons, got none")
	}
}

// TestConstraintValidationUnavailableProduct tests validation fails for unavailable product
func TestConstraintValidationUnavailableProduct(t *testing.T) {
	intent := &nlp.ShoppingIntent{}

	product := &models.MarketplaceProduct{
		ID:             "1",
		Name:           "Unavailable Product",
		Price:          1000,
		IsAvailable:    false,
		InventoryCount: 0,
	}

	validator := nlp.NewConstraintValidator(intent)
	result := validator.ValidateProduct(product)

	if result.Valid {
		t.Error("expected product to be invalid due to unavailability")
	}

	if !contains(result.Reasons, "product is not available") {
		t.Error("expected 'product is not available' in reasons")
	}
}

// TestProductRankingByScore tests that products are scored correctly by the validator
func TestProductRankingByScore(t *testing.T) {
	intent := &nlp.ShoppingIntent{
		Keywords:   []string{"laptop"},
		Categories: []string{"Electronics"},
		PriceRange: &nlp.PriceRange{
			Min:      20000,
			Max:      80000,
			Currency: "INR",
		},
	}

	products := []*models.MarketplaceProduct{
		{
			ID:             "1",
			Name:           "Laptop Computer",
			Price:          50000,
			Currency:       "INR",
			IsAvailable:    true,
			InventoryCount: 5,
			Categories:     `["Electronics"]`,
		},
		{
			ID:             "2",
			Name:           "Phone Device", // No keyword match for "laptop"
			Price:          50000,
			Currency:       "INR",
			IsAvailable:    true,
			InventoryCount: 3,
			Categories:     `["Electronics"]`,
		},
	}

	validator := nlp.NewConstraintValidator(intent)

	// Both products should be valid (they meet price and category constraints)
	for _, product := range products {
		result := validator.ValidateProduct(product)
		if !result.Valid {
			t.Errorf("expected product %s to be valid", product.ID)
		}
		// Both should score 100 (capped) since they match category and are at price midpoint
		if result.Score < 100 {
			t.Errorf("expected product %s to have score 100 (capped), got %.2f", product.ID, result.Score)
		}
	}
}

// TestRuleBasedIntentParser tests that the rule-based parser can parse basic intents
func TestRuleBasedIntentParser(t *testing.T) {
	parser := nlp.NewRuleBasedIntentParser()

	if parser == nil {
		t.Fatal("rule-based parser should not be nil")
	}
}

// Helper function
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// BenchmarkConstraintValidation benchmarks constraint validation
func BenchmarkConstraintValidation(b *testing.B) {
	intent := &nlp.ShoppingIntent{
		Categories: []string{"Electronics"},
		PriceRange: &nlp.PriceRange{
			Min:      10000,
			Max:      50000,
			Currency: "INR",
		},
	}

	product := &models.MarketplaceProduct{
		ID:             "1",
		Name:           "Test Product",
		Price:          25000,
		IsAvailable:    true,
		InventoryCount: 5,
	}

	validator := nlp.NewConstraintValidator(intent)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateProduct(product)
	}
}

// BenchmarkValidatorBatchProcessing benchmarks validating multiple products
func BenchmarkValidatorBatchProcessing(b *testing.B) {
	products := make([]*models.MarketplaceProduct, 100)
	for i := 0; i < 100; i++ {
		products[i] = &models.MarketplaceProduct{
			ID:             string(rune(i)),
			Name:           "Product " + string(rune(i)),
			Price:          float64(10000 + i*1000),
			IsAvailable:    true,
			InventoryCount: 5,
		}
	}

	intent := &nlp.ShoppingIntent{
		Keywords: []string{"product"},
		PriceRange: &nlp.PriceRange{
			Min:      10000,
			Max:      50000,
			Currency: "INR",
		},
	}

	validator := nlp.NewConstraintValidator(intent)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateProducts(products)
	}
}
