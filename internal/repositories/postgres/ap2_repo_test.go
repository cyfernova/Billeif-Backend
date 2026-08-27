package postgres_test

import (
	"context"
	"testing"
	"time"

	"invoice-backend/internal/models"
	postgresrepo "invoice-backend/internal/repositories/postgres"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBargainingRoundClaimIsDurableLeaseBoundAndCompletable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE bargaining_round_claims (
			negotiation_id TEXT NOT NULL,
			round_number INTEGER NOT NULL,
			lease_owner TEXT NOT NULL,
			lease_expires_at DATETIME NOT NULL,
			completed_at DATETIME,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			PRIMARY KEY (negotiation_id, round_number)
		)
	`).Error; err != nil {
		t.Fatalf("create bargaining claim table: %v", err)
	}

	repo := postgresrepo.NewAP2Repository(db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)

	claimed, err := repo.ClaimBargainingRound(ctx, "negotiation-1", 1, "worker-a", now, now.Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v; want true, nil", claimed, err)
	}
	claimed, err = repo.ClaimBargainingRound(ctx, "negotiation-1", 1, "worker-b", now.Add(30*time.Second), now.Add(90*time.Second))
	if err != nil || claimed {
		t.Fatalf("unexpired competing claim = %v, %v; want false, nil", claimed, err)
	}
	claimed, err = repo.ClaimBargainingRound(ctx, "negotiation-1", 1, "worker-b", now.Add(61*time.Second), now.Add(121*time.Second))
	if err != nil || !claimed {
		t.Fatalf("expired reclaim = %v, %v; want true, nil", claimed, err)
	}

	completed, err := repo.CompleteBargainingRoundClaim(ctx, "negotiation-1", 1, "worker-a", now.Add(70*time.Second))
	if err != nil || completed {
		t.Fatalf("stale-owner completion = %v, %v; want false, nil", completed, err)
	}
	completed, err = repo.CompleteBargainingRoundClaim(ctx, "negotiation-1", 1, "worker-b", now.Add(70*time.Second))
	if err != nil || !completed {
		t.Fatalf("current-owner completion = %v, %v; want true, nil", completed, err)
	}
	claimed, err = repo.ClaimBargainingRound(ctx, "negotiation-1", 1, "worker-c", now.Add(3*time.Minute), now.Add(4*time.Minute))
	if err != nil || claimed {
		t.Fatalf("completed reclaim = %v, %v; want false, nil", claimed, err)
	}
}

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

func TestDeleteCapabilityScopesByAgent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE agent_capabilities (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			capability_type TEXT NOT NULL,
			description TEXT,
			config TEXT NOT NULL,
			created_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create agent capabilities table: %v", err)
	}

	if err := db.Create(&models.AgentCapability{
		ID:             "capability-owned-by-agent-a",
		AgentID:        "agent-a",
		CapabilityType: "search_products",
		Config:         "{}",
	}).Error; err != nil {
		t.Fatalf("seed agent-a capability: %v", err)
	}
	if err := db.Create(&models.AgentCapability{
		ID:             "capability-owned-by-agent-b",
		AgentID:        "agent-b",
		CapabilityType: "create_cart",
		Config:         "{}",
	}).Error; err != nil {
		t.Fatalf("seed agent-b capability: %v", err)
	}

	repo := postgresrepo.NewAP2Repository(db)
	ctx := context.Background()
	if err := repo.DeleteCapability(ctx, "agent-a", "capability-owned-by-agent-a"); err != nil {
		t.Fatalf("delete owned capability: %v", err)
	}

	var remaining models.AgentCapability
	if err := db.Where("id = ?", "capability-owned-by-agent-b").First(&remaining).Error; err != nil {
		t.Fatalf("load foreign capability: %v", err)
	}
	if remaining.AgentID != "agent-b" {
		t.Fatalf("foreign capability agent_id = %q, want agent-b", remaining.AgentID)
	}

	if err := repo.DeleteCapability(ctx, "agent-a", "capability-owned-by-agent-b"); err == nil {
		t.Fatal("cross-agent capability deletion succeeded")
	}
	if err := db.Where("id = ?", "capability-owned-by-agent-b").First(&remaining).Error; err != nil {
		t.Fatalf("foreign capability was deleted: %v", err)
	}
}
