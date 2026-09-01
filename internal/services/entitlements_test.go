package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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

func TestEntitlementServiceResolveByBusinessDefaultsToRestrictedFreePlan(t *testing.T) {
	db := newEntitlementsTestDB(t)
	service := NewEntitlementService(&config.Config{}, db, postgresrepo.NewSubscriptionRepository(db), logger.New())

	entitlements, err := service.ResolveByBusiness(context.Background(), "missing-business")
	require.NoError(t, err)
	require.False(t, entitlements.EInvoiceEnabled)
	require.False(t, entitlements.EWayBillEnabled)
	require.False(t, entitlements.POSEnabled)
}

func TestEntitlementServiceRejectsCanceledExpiredAndElapsedSubscriptions(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		endDate *time.Time
	}{
		{name: "canceled", status: "canceled"},
		{name: "expired", status: "expired"},
		{name: "elapsed", status: "active", endDate: entitlementTimePointer(time.Now().UTC().Add(-time.Minute))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newEntitlementsTestDB(t)
			repo := postgresrepo.NewSubscriptionRepository(db)
			require.NoError(t, repo.Create(context.Background(), &models.Subscription{
				ID: "sub-1", BusinessID: "biz-1", Plan: "enterprise", PlanCode: "biz",
				Status: tt.status, StartDate: time.Now().UTC().Add(-time.Hour), EndDate: tt.endDate,
			}))

			service := NewEntitlementService(&config.Config{}, db, repo, logger.New())
			entitlements, err := service.ResolveByBusiness(context.Background(), "biz-1")
			require.NoError(t, err)
			require.False(t, entitlements.EInvoiceEnabled)
			require.False(t, entitlements.POSEnabled)
		})
	}
}

func TestEntitlementServiceReserveFeatureReturnsStructuredErrors(t *testing.T) {
	db := newEntitlementsTestDB(t)
	service := NewEntitlementService(&config.Config{}, db, postgresrepo.NewSubscriptionRepository(db), logger.New())

	err := db.Transaction(func(tx *gorm.DB) error {
		return service.ReserveFeatureTx(context.Background(), tx, "missing-business", FeatureEInvoice, 1)
	})
	var featureErr *FeatureUnavailableError
	require.ErrorAs(t, err, &featureErr)
	require.Equal(t, "feature_disabled", featureErr.Code)
	require.Equal(t, FeatureEInvoice, featureErr.Feature)
	require.Equal(t, "free", featureErr.PlanID)

	seedActiveSubscription(t, db, "biz-1", "starter", "pro", nil)
	limit := subscriptionPlanForCode("pro").Quotas[QuotaEInvoiceMonthly]
	require.NoError(t, db.Exec(
		"INSERT INTO subscription_quota_usage (business_id, feature_key, period_start, used_value) VALUES (?, ?, ?, ?)",
		"biz-1", FeatureEInvoice, currentQuotaPeriodStart(time.Now().UTC()), limit,
	).Error)

	err = db.Transaction(func(tx *gorm.DB) error {
		return service.ReserveFeatureTx(context.Background(), tx, "biz-1", FeatureEInvoice, 1)
	})
	var quotaErr *QuotaExceededError
	require.ErrorAs(t, err, &quotaErr)
	require.Equal(t, "quota_exceeded", quotaErr.Code)
	require.Equal(t, FeatureEInvoice, quotaErr.Feature)
	require.Equal(t, limit, quotaErr.Limit)
	require.Equal(t, limit, quotaErr.Used)
	require.Equal(t, "pro_monthly", quotaErr.PlanID)
}

func TestEntitlementServiceReserveFeatureIsAtomicAtFinalSlot(t *testing.T) {
	db := newEntitlementsTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	seedActiveSubscription(t, db, "biz-1", "starter", "pro", nil)
	service := NewEntitlementService(&config.Config{}, db, postgresrepo.NewSubscriptionRepository(db), logger.New())
	limit := subscriptionPlanForCode("pro").Quotas[QuotaEInvoiceMonthly]
	require.NoError(t, db.Exec(
		"INSERT INTO subscription_quota_usage (business_id, feature_key, period_start, used_value) VALUES (?, ?, ?, ?)",
		"biz-1", FeatureEInvoice, currentQuotaPeriodStart(time.Now().UTC()), limit-1,
	).Error)

	start := make(chan struct{})
	errorsByClaim := make(chan error, 2)
	var wg sync.WaitGroup
	for claim := 0; claim < 2; claim++ {
		claim := claim
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errorsByClaim <- db.Transaction(func(tx *gorm.DB) error {
				if err := service.ReserveFeatureTx(context.Background(), tx, "biz-1", FeatureEInvoice, 1); err != nil {
					return err
				}
				return tx.Exec("INSERT INTO governed_actions (id, business_id, feature_key) VALUES (?, ?, ?)", claim+1, "biz-1", FeatureEInvoice).Error
			})
		}()
	}
	close(start)
	wg.Wait()
	close(errorsByClaim)

	successes := 0
	quotaFailures := 0
	for claimErr := range errorsByClaim {
		if claimErr == nil {
			successes++
			continue
		}
		var quotaErr *QuotaExceededError
		if errors.As(claimErr, &quotaErr) {
			quotaFailures++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, quotaFailures)

	var actionCount int64
	require.NoError(t, db.Table("governed_actions").Count(&actionCount).Error)
	require.Equal(t, int64(1), actionCount)
}

func TestEntitlementServiceDowngradeUsesCurrentCatalogLimit(t *testing.T) {
	db := newEntitlementsTestDB(t)
	seedActiveSubscription(t, db, "biz-1", "starter", "pro", nil)
	service := NewEntitlementService(&config.Config{}, db, postgresrepo.NewSubscriptionRepository(db), logger.New())
	limit := subscriptionPlanForCode("pro").Quotas[QuotaEInvoiceMonthly]
	require.NoError(t, db.Exec(
		"INSERT INTO subscription_quota_usage (business_id, feature_key, period_start, used_value) VALUES (?, ?, ?, ?)",
		"biz-1", FeatureEInvoice, currentQuotaPeriodStart(time.Now().UTC()), limit+25,
	).Error)

	err := db.Transaction(func(tx *gorm.DB) error {
		return service.ReserveFeatureTx(context.Background(), tx, "biz-1", FeatureEInvoice, 1)
	})
	var quotaErr *QuotaExceededError
	require.ErrorAs(t, err, &quotaErr)
	require.Equal(t, limit, quotaErr.Limit)
	require.Equal(t, limit+25, quotaErr.Used)
}

func TestEntitlementServiceInspectFeatureReportsCurrentQuotaWithoutReserving(t *testing.T) {
	db := newEntitlementsTestDB(t)
	seedActiveSubscription(t, db, "biz-1", "starter", "pro", nil)
	service := NewEntitlementService(&config.Config{}, db, postgresrepo.NewSubscriptionRepository(db), logger.New())
	limit := subscriptionPlanForCode("pro").Quotas[QuotaEInvoiceMonthly]
	require.NoError(t, db.Exec(
		"INSERT INTO subscription_quota_usage (business_id, feature_key, period_start, used_value) VALUES (?, ?, ?, ?)",
		"biz-1", FeatureEInvoice, currentQuotaPeriodStart(time.Now().UTC()), limit-1,
	).Error)

	access, err := service.InspectFeature(context.Background(), "biz-1", FeatureEInvoice)
	require.NoError(t, err)
	require.True(t, access.Required)
	require.True(t, access.Entitled)
	require.True(t, access.Quota.Limited)
	require.True(t, access.Quota.Available)
	require.Equal(t, int64(1), access.Quota.Remaining)

	access, err = service.InspectFeature(context.Background(), "biz-1", FeatureEInvoice)
	require.NoError(t, err)
	require.Equal(t, int64(1), access.Quota.Remaining, "inspection must not reserve quota")

	access, err = service.InspectFeature(context.Background(), "missing-business", FeatureEInvoice)
	require.NoError(t, err)
	require.False(t, access.Entitled)
	require.False(t, access.Quota.Available)
}

func TestEntitlementQuotaUsesVerifiedSubscriptionPeriodStart(t *testing.T) {
	db := newEntitlementsTestDB(t)
	periodStart := time.Date(2026, 8, 20, 8, 30, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)
	seedActiveSubscription(t, db, "biz-period", "starter", "pro", &periodEnd)
	require.NoError(t, db.Model(&models.Subscription{}).Where("business_id = ?", "biz-period").Updates(map[string]interface{}{
		"billing_mode": "renewable", "period_start": periodStart, "period_end": periodEnd, "last_provider_paid_count": 1,
	}).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO subscription_quota_usage (business_id, feature_key, period_start, used_value) VALUES (?, ?, ?, ?)",
		"biz-period", FeatureEInvoice, periodStart, int64(7),
	).Error)
	service := NewEntitlementService(&config.Config{}, db, postgresrepo.NewSubscriptionRepository(db), logger.New())

	access, err := service.InspectFeature(context.Background(), "biz-period", FeatureEInvoice)

	require.NoError(t, err)
	require.EqualValues(t, 7, access.Quota.Used)
}

func TestEntitlementServiceInspectDriveStorageUsesTenantScopedAssetUsageInMB(t *testing.T) {
	db := newEntitlementsTestDB(t)
	seedActiveSubscription(t, db, "biz-below", "starter", "pro", nil)
	seedActiveSubscription(t, db, "biz-exact", "starter", "pro", nil)
	limitMB := subscriptionPlanForCode("pro").Quotas[QuotaStorageMB]
	require.NoError(t, db.Exec(
		"INSERT INTO drive_assets (id, business_id, size_bytes) VALUES (?, ?, ?), (?, ?, ?), (?, ?, ?)",
		"asset-below", "biz-below", int64(10*1024*1024),
		"asset-other", "other-business", int64(900*1024*1024),
		"asset-exact", "biz-exact", limitMB*1024*1024,
	).Error)
	service := NewEntitlementService(&config.Config{}, db, postgresrepo.NewSubscriptionRepository(db), logger.New())

	below, err := service.InspectFeature(context.Background(), "biz-below", FeatureDriveStorageMB)
	require.NoError(t, err)
	require.Equal(t, "MB", below.Quota.Unit)
	require.Equal(t, limitMB, below.Quota.Limit)
	require.Equal(t, int64(10), below.Quota.Used)
	require.Equal(t, limitMB-10, below.Quota.Remaining)
	require.True(t, below.Quota.Available)

	exact, err := service.InspectFeature(context.Background(), "biz-exact", FeatureDriveStorageMB)
	require.NoError(t, err)
	require.Equal(t, limitMB, exact.Quota.Used)
	require.Zero(t, exact.Quota.Remaining)
	require.False(t, exact.Quota.Available)
}

func TestEntitlementReservationRollsBackWithGovernedWrite(t *testing.T) {
	db := newEntitlementsTestDB(t)
	seedActiveSubscription(t, db, "biz-1", "starter", "pro", nil)
	service := NewEntitlementService(&config.Config{}, db, postgresrepo.NewSubscriptionRepository(db), logger.New())

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := service.ReserveFeatureTx(context.Background(), tx, "biz-1", FeatureEInvoice, 1); err != nil {
			return err
		}
		return errors.New("governed write failed")
	})
	require.EqualError(t, err, "governed write failed")

	var usageCount int64
	require.NoError(t, db.Model(&models.SubscriptionQuotaUsage{}).Count(&usageCount).Error)
	require.Zero(t, usageCount)
}

func newEntitlementsTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_busy_timeout=5000", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	statements := []string{
		`CREATE TABLE subscriptions (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			plan TEXT NOT NULL,
			plan_code TEXT,
			catalog_version TEXT,
			status TEXT NOT NULL,
			billing_mode TEXT,
			provider_mode TEXT,
			provider_customer_id TEXT,
			provider_subscription_id TEXT,
			provider_plan_id TEXT,
			max_invoices INTEGER,
			max_customers INTEGER,
			max_users INTEGER,
			max_storage_mb INTEGER,
			start_date DATETIME NOT NULL,
			end_date DATETIME,
			next_billing_date DATETIME,
			period_start DATETIME,
			period_end DATETIME,
			next_renewal_at DATETIME,
			grace_deadline DATETIME,
			cancel_at_period_end NUMERIC,
			cancellation_effective_at DATETIME,
			cancelled_at DATETIME,
			pending_plan_id TEXT,
			pending_provider_plan_id TEXT,
			pending_plan_effective_at DATETIME,
			last_provider_event_at DATETIME,
			last_provider_paid_count INTEGER,
			reconciliation_code TEXT,
			lifecycle_version INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE UNIQUE INDEX idx_test_subscriptions_business ON subscriptions (business_id) WHERE deleted_at IS NULL`,
		`CREATE TABLE subscription_quota_usage (
			business_id TEXT NOT NULL,
			feature_key TEXT NOT NULL,
			period_start DATETIME NOT NULL,
			used_value INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME,
			PRIMARY KEY (business_id, feature_key, period_start)
		)`,
		`CREATE TABLE governed_actions (id INTEGER PRIMARY KEY, business_id TEXT NOT NULL, feature_key TEXT NOT NULL)`,
		`CREATE TABLE drive_assets (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			deleted_at DATETIME
		)`,
	}
	for _, statement := range statements {
		require.NoError(t, db.Exec(statement).Error)
	}
	return db
}

func seedActiveSubscription(t *testing.T, db *gorm.DB, businessID, plan, planCode string, endDate *time.Time) {
	t.Helper()
	require.NoError(t, db.Create(&models.Subscription{
		ID: "sub-" + businessID, BusinessID: businessID, Plan: plan, PlanCode: planCode,
		Status: "active", StartDate: time.Now().UTC().Add(-time.Hour), EndDate: endDate,
	}).Error)
}

func entitlementTimePointer(value time.Time) *time.Time {
	return &value
}
