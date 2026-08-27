package postgres_test

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
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
		CREATE TABLE bargaining_negotiations (
			id TEXT PRIMARY KEY,
			session_id TEXT,
			user_id TEXT NOT NULL,
			business_id TEXT NOT NULL,
			status TEXT NOT NULL,
			current_amount REAL NOT NULL DEFAULT 0,
			rounds INTEGER NOT NULL DEFAULT 0,
			completed_at DATETIME
		);
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
		t.Fatalf("create bargaining tables: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO bargaining_negotiations (
			id, session_id, user_id, business_id, status
		) VALUES (?, ?, ?, ?, ?)
	`, "negotiation-1", "session-1", "user-1", "business-1", "running").Error; err != nil {
		t.Fatalf("seed bargaining negotiation: %v", err)
	}

	repo := postgresrepo.NewAP2Repository(db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)

	claimed, err := repo.ClaimBargainingRound(ctx, "wrong-session", "negotiation-1", 1, "worker-attacker", now, now.Add(time.Minute))
	if err != nil || claimed {
		t.Fatalf("mismatched tuple claim = %v, %v; want false, nil", claimed, err)
	}
	claimed, err = repo.ClaimBargainingRound(ctx, "session-1", "negotiation-1", 1, "worker-a", now, now.Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v; want true, nil", claimed, err)
	}
	claimed, err = repo.ClaimBargainingRound(ctx, "session-1", "negotiation-1", 1, "worker-b", now.Add(30*time.Second), now.Add(90*time.Second))
	if err != nil || claimed {
		t.Fatalf("unexpired competing claim = %v, %v; want false, nil", claimed, err)
	}
	claimed, err = repo.ClaimBargainingRound(ctx, "session-1", "negotiation-1", 1, "worker-b", now.Add(61*time.Second), now.Add(121*time.Second))
	if err != nil || !claimed {
		t.Fatalf("expired reclaim = %v, %v; want true, nil", claimed, err)
	}

	completed, err := repo.CompleteBargainingRoundClaim(ctx, "session-1", "negotiation-1", 1, "worker-a", now.Add(70*time.Second))
	if err != nil || completed {
		t.Fatalf("stale-owner completion = %v, %v; want false, nil", completed, err)
	}
	completed, err = repo.CompleteBargainingRoundClaim(ctx, "session-1", "negotiation-1", 1, "worker-b", now.Add(70*time.Second))
	if err != nil || !completed {
		t.Fatalf("current-owner completion = %v, %v; want true, nil", completed, err)
	}
	claimed, err = repo.ClaimBargainingRound(ctx, "session-1", "negotiation-1", 1, "worker-c", now.Add(3*time.Minute), now.Add(4*time.Minute))
	if err != nil || claimed {
		t.Fatalf("completed reclaim = %v, %v; want false, nil", claimed, err)
	}
}

func TestBargainingOwnershipAndStoppedStateAreEnforcedByRepositoryMutations(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE bargaining_negotiations (
			id TEXT PRIMARY KEY,
			session_id TEXT,
			buyer_agent_id TEXT NOT NULL,
			seller_agent_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			business_id TEXT NOT NULL,
			initial_amount REAL NOT NULL,
			current_amount REAL NOT NULL,
			buyer_volatility REAL NOT NULL DEFAULT 0,
			seller_volatility REAL NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			rounds INTEGER NOT NULL DEFAULT 0,
			max_rounds INTEGER NOT NULL DEFAULT 5,
			expires_at DATETIME NOT NULL,
			metadata TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME,
			updated_at DATETIME,
			completed_at DATETIME
		);
		CREATE TABLE bargaining_rounds (
			id TEXT PRIMARY KEY,
			negotiation_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			round_number INTEGER NOT NULL,
			proposed_amount REAL NOT NULL,
			previous_amount REAL NOT NULL,
			agent_type TEXT NOT NULL,
			action TEXT NOT NULL,
			reason TEXT,
			volatility_factor REAL,
			metadata TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME,
			UNIQUE (negotiation_id, round_number)
		);
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
		t.Fatalf("create bargaining tables: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO bargaining_negotiations (
			id, session_id, buyer_agent_id, seller_agent_id, user_id, business_id,
			initial_amount, current_amount, status, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "negotiation-1", "session-1", "buyer-1", "seller-1", "user-1", "business-1", 100, 100, "running", time.Now().Add(time.Hour)).Error; err != nil {
		t.Fatalf("seed bargaining negotiation: %v", err)
	}

	repo := postgresrepo.NewAP2Repository(db)
	ctx := context.Background()
	if _, err := repo.GetBargainingNegotiationBySessionIDForScope(ctx, "session-1", "attacker", "business-1"); !errors.Is(err, interfaces.ErrA2ANegotiationScopeNotFound) {
		t.Fatalf("foreign user scoped lookup error = %v, want scoped not found", err)
	}
	if _, err := repo.GetBargainingNegotiationByIDForScope(ctx, "negotiation-1", "user-1", "wrong-business"); !errors.Is(err, interfaces.ErrA2ANegotiationScopeNotFound) {
		t.Fatalf("foreign business scoped lookup error = %v, want scoped not found", err)
	}
	if _, err := repo.GetBargainingNegotiationBySessionAndID(ctx, "wrong-session", "negotiation-1"); !errors.Is(err, interfaces.ErrA2ANegotiationScopeNotFound) {
		t.Fatalf("mismatched session/negotiation lookup error = %v, want scoped not found", err)
	}
	stopped, err := repo.StopBargainingNegotiationForScope(ctx, "negotiation-1", "attacker", "business-1", time.Now())
	if err != nil || stopped {
		t.Fatalf("foreign stop = %v, %v; want false, nil", stopped, err)
	}
	stopped, err = repo.StopBargainingNegotiationForScope(ctx, "negotiation-1", "user-1", "business-1", time.Now())
	if err != nil || !stopped {
		t.Fatalf("owner stop = %v, %v; want true, nil", stopped, err)
	}

	round := &models.BargainingRound{
		ID: "round-1", NegotiationID: "negotiation-1", AgentID: "buyer-1",
		RoundNumber: 1, ProposedAmount: 90, PreviousAmount: 100,
		AgentType: "buyer", Action: "counteroffer", Metadata: "{}",
	}
	if err := repo.CreateBargainingRound(ctx, round); !errors.Is(err, interfaces.ErrBargainingNegotiationNotActive) {
		t.Fatalf("round creation after stop error = %v", err)
	}
	if err := repo.UpdateNegotiationAmountAndRounds(ctx, "negotiation-1", 90, 1, "in_progress"); !errors.Is(err, interfaces.ErrBargainingNegotiationNotActive) {
		t.Fatalf("amount update after stop error = %v", err)
	}
	if err := repo.CompleteNegotiation(ctx, "negotiation-1", "accepted", 90, ptrTime(time.Now())); !errors.Is(err, interfaces.ErrBargainingNegotiationNotActive) {
		t.Fatalf("completion after stop error = %v", err)
	}
	if err := repo.UpdateNegotiationStatus(ctx, "negotiation-1", "expired"); !errors.Is(err, interfaces.ErrBargainingNegotiationNotActive) {
		t.Fatalf("status update after stop error = %v", err)
	}
	claimed, err := repo.ClaimBargainingRound(ctx, "session-1", "negotiation-1", 1, "worker", time.Now(), time.Now().Add(time.Minute))
	if err != nil || claimed {
		t.Fatalf("claim after stop = %v, %v; want false, nil", claimed, err)
	}

	var status string
	var amount float64
	var rounds int
	if err := db.Raw(`SELECT status, current_amount, rounds FROM bargaining_negotiations WHERE id = ?`, "negotiation-1").Row().Scan(&status, &amount, &rounds); err != nil {
		t.Fatalf("load stopped negotiation: %v", err)
	}
	if status != "stopped" || amount != 100 || rounds != 0 {
		t.Fatalf("stopped negotiation state = %q/%v/%d", status, amount, rounds)
	}
	var roundCount int64
	if err := db.Table("bargaining_rounds").Count(&roundCount).Error; err != nil {
		t.Fatalf("count rounds: %v", err)
	}
	if roundCount != 0 {
		t.Fatalf("round count after stopped mutation = %d, want 0", roundCount)
	}
}

func TestA2AScopedRepositoryLookupsDoNotClassifyDatabaseFailuresAsNotFound(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sqlite db: %v", err)
	}

	repo := postgresrepo.NewAP2Repository(db)
	ctx := context.Background()
	lookups := []struct {
		name string
		run  func() error
	}{
		{
			name: "agent owner and business",
			run: func() error {
				_, err := repo.GetAgentByIDForOwnerAndBusiness(ctx, "agent-1", "user-1", "business-1")
				return err
			},
		},
		{
			name: "negotiation ID scope",
			run: func() error {
				_, err := repo.GetBargainingNegotiationByIDForScope(ctx, "negotiation-1", "user-1", "business-1")
				return err
			},
		},
		{
			name: "negotiation session scope",
			run: func() error {
				_, err := repo.GetBargainingNegotiationBySessionIDForScope(ctx, "session-1", "user-1", "business-1")
				return err
			},
		},
		{
			name: "exact tuple",
			run: func() error {
				_, err := repo.GetBargainingNegotiationBySessionAndID(ctx, "session-1", "negotiation-1")
				return err
			},
		},
	}
	for _, lookup := range lookups {
		t.Run(lookup.name, func(t *testing.T) {
			err := lookup.run()
			if err == nil {
				t.Fatal("database failure returned nil error")
			}
			if errors.Is(err, interfaces.ErrA2ANegotiationScopeNotFound) {
				t.Fatalf("database failure was classified as scoped not found: %v", err)
			}
		})
	}
}

func ptrTime(value time.Time) *time.Time { return &value }

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
