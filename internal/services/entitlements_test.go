package services

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	postgresrepo "invoice-backend/internal/repositories/postgres"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEntitlementService_ResolveByBusinessDefaultsToFree(t *testing.T) {
	db := newEntitlementsTestDB(t)
	service := NewEntitlementService(&config.Config{}, db, postgresrepo.NewSubscriptionRepository(db), logger.New())

	entitlements, err := service.ResolveByBusiness(context.Background(), "missing-business")
	require.NoError(t, err)
	require.False(t, entitlements.EInvoiceEnabled)
	require.False(t, entitlements.EWayBillEnabled)
	require.False(t, entitlements.POSEnabled)
}

func TestEntitlementService_EnsureFeatureAppliesMonthlyLimit(t *testing.T) {
	db := newEntitlementsTestDB(t)
	repo := postgresrepo.NewSubscriptionRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.Create(ctx, &models.Subscription{
		ID:         "sub-1",
		BusinessID: "biz-1",
		Plan:       "starter",
		Status:     "active",
		StartDate:  time.Now().UTC().Add(-24 * time.Hour),
	}))
	require.NoError(t, db.WithContext(ctx).Exec(
		"INSERT INTO einvoice_records (id, business_id, document_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"einv-1", "biz-1", "doc-1", models.EInvoiceStatusGenerated, time.Now().UTC(), time.Now().UTC(),
	).Error)

	service := NewEntitlementService(&config.Config{
		Entitlements: config.EntitlementsConfig{
			JSON: `{"starter":{"einvoice_enabled":true,"einvoice_limit":1,"ewaybill_enabled":true,"ewaybill_limit":5,"bulk_gst_enabled":false,"gst_api_enabled":false,"pos_enabled":false}}`,
		},
	}, db, repo, logger.New())

	err := service.EnsureFeature(ctx, "biz-1", FeatureEInvoice)
	require.Error(t, err)
	require.Contains(t, err.Error(), "monthly quota exceeded")
}

func newEntitlementsTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	statements := []string{
		`CREATE TABLE subscriptions (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			plan TEXT NOT NULL,
			status TEXT NOT NULL,
			max_invoices INTEGER,
			max_customers INTEGER,
			max_users INTEGER,
			max_storage_mb INTEGER,
			start_date DATETIME NOT NULL,
			end_date DATETIME,
			next_billing_date DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE einvoice_records (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			document_id TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE ewaybill_records (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			document_id TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
	}
	for _, statement := range statements {
		require.NoError(t, db.Exec(statement).Error)
	}
	return db
}
