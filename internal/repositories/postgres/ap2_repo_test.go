package postgres_test

import (
	"context"
	"testing"

	"invoice-backend/internal/models"
	postgresrepo "invoice-backend/internal/repositories/postgres"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSearchMarketplaceProductsEmptyQueryUsesMarketplaceProductSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE marketplace_products (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			product_id TEXT,
			name TEXT NOT NULL,
			description TEXT,
			price DECIMAL(15,2) NOT NULL,
			currency TEXT NOT NULL DEFAULT 'INR',
			inventory_count INTEGER DEFAULT 0,
			reserved_inventory_count INTEGER DEFAULT 0,
			is_available BOOLEAN DEFAULT TRUE,
			images TEXT DEFAULT '[]',
			categories TEXT DEFAULT '[]',
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create marketplace product table: %v", err)
	}

	product := &models.MarketplaceProduct{
		ID:             "product-1",
		AgentID:        "agent-1",
		Name:           "Desk lamp",
		Price:          1200,
		Currency:       "INR",
		InventoryCount: 4,
		IsAvailable:    true,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("seed marketplace product: %v", err)
	}

	repo := postgresrepo.NewAP2Repository(db)
	products, total, err := repo.SearchMarketplaceProducts(context.Background(), "", 1, 10)
	if err != nil {
		t.Fatalf("search marketplace products: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if len(products) != 1 || products[0].ID != product.ID {
		t.Fatalf("products = %#v, want seeded product", products)
	}
}
