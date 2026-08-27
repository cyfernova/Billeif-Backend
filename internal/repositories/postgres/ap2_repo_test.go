package postgres_test

import (
	"context"
	"regexp"
	"testing"
	"time"

	"invoice-backend/internal/models"
	postgresrepo "invoice-backend/internal/repositories/postgres"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestGetOrderByIDForUserUsesOwnerPredicate(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("open sqlmock: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlmock: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("SQL expectations: %v", err)
		}
	})

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}

	orderID := "order-1"
	userID := "user-1"
	query := regexp.QuoteMeta(`SELECT * FROM "marketplace_orders" WHERE id = $1 AND user_id = $2 ORDER BY "marketplace_orders"."id" LIMIT $3`)
	mock.ExpectQuery(query).
		WithArgs(orderID, userID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id"}).AddRow(orderID, userID))
	mock.ExpectClose()

	order, err := postgresrepo.NewAP2Repository(db).GetOrderByIDForUser(context.Background(), orderID, userID)
	if err != nil {
		t.Fatalf("get owned order: %v", err)
	}
	if order.ID != orderID || order.UserID != userID {
		t.Fatalf("order = %#v, want order %q owned by %q", order, orderID, userID)
	}
}

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
