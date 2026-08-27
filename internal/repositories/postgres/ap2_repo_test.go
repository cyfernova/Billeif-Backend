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

func TestGetBargainingNegotiationByIDForActorScopesToUserOrActiveAgentBusiness(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE agents (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			business_id TEXT NOT NULL,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			deleted_at DATETIME
		);
		CREATE TABLE bargaining_negotiations (
			id TEXT PRIMARY KEY,
			buyer_agent_id TEXT NOT NULL,
			seller_agent_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			initial_amount REAL NOT NULL,
			current_amount REAL NOT NULL,
			buyer_volatility REAL NOT NULL DEFAULT 0,
			seller_volatility REAL NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			rounds INTEGER NOT NULL DEFAULT 0,
			max_rounds INTEGER NOT NULL,
			expires_at DATETIME NOT NULL,
			metadata TEXT NOT NULL,
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create bargaining tables: %v", err)
	}

	const (
		buyerID       = "buyer-1"
		buyerBusiness = "buyer-business"
		negotiationID = "negotiation-1"
	)
	if err := db.Exec("INSERT INTO agents (id, owner_id, business_id, name, type) VALUES (?, ?, ?, ?, ?), (?, ?, ?, ?, ?)",
		buyerID, "buyer-owner", buyerBusiness, "Buyer", "shopping",
		"seller-1", "seller-owner", "seller-business", "Seller", "merchant",
	).Error; err != nil {
		t.Fatalf("seed agents: %v", err)
	}
	if err := db.Exec("INSERT INTO bargaining_negotiations (id, buyer_agent_id, seller_agent_id, user_id, initial_amount, current_amount, status, max_rounds, expires_at, metadata) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		negotiationID, buyerID, "seller-1", "negotiation-user", 100, 100, "initiated", 5, time.Now().Add(time.Hour), "{}",
	).Error; err != nil {
		t.Fatalf("seed negotiation: %v", err)
	}

	repo := postgresrepo.NewAP2Repository(db)
	ctx := context.Background()

	if _, err := repo.GetBargainingNegotiationByIDForActor(ctx, negotiationID, "negotiation-user", "foreign-business"); err != nil {
		t.Fatalf("same user lookup: %v", err)
	}
	if _, err := repo.GetBargainingNegotiationByIDForActor(ctx, negotiationID, "foreign-user", buyerBusiness); err != nil {
		t.Fatalf("matching active agent business lookup: %v", err)
	}
	if _, err := repo.GetBargainingNegotiationByIDForActor(ctx, negotiationID, "foreign-user", "foreign-business"); err == nil {
		t.Fatal("cross-user and cross-business lookup unexpectedly succeeded")
	}
	if err := db.Exec("UPDATE agents SET deleted_at = ? WHERE id = ?", time.Now(), buyerID).Error; err != nil {
		t.Fatalf("soft delete buyer agent: %v", err)
	}
	if _, err := repo.GetBargainingNegotiationByIDForActor(ctx, negotiationID, "foreign-user", buyerBusiness); err == nil {
		t.Fatal("soft-deleted agent business lookup unexpectedly succeeded")
	}
}
