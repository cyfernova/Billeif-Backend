package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/DATA-DOG/go-sqlmock"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCapabilityProviderHealthUpsertIsTenantKeyedAndMonotonic(t *testing.T) {
	repository, mock, closeDatabase := newCapabilityProviderHealthSQLMockRepository(t)
	defer closeDatabase()
	observedAt := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	freshUntil := observedAt.Add(24 * time.Hour)
	retryAt := observedAt.Add(time.Minute)
	mock.ExpectExec(`INSERT INTO capability_provider_health_snapshots[\s\S]*ON CONFLICT \(business_id, provider_key\)[\s\S]*WHERE capability_provider_health_snapshots\.observed_at < EXCLUDED\.observed_at`).
		WithArgs("business-a", "gst_provider", "degraded", observedAt, freshUntil, &retryAt, "provider_rate_limited", observedAt, observedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repository.UpsertMonotonic(context.Background(), &models.CapabilityProviderHealthSnapshot{
		BusinessID: "business-a", ProviderKey: "gst_provider", Status: "degraded",
		ObservedAt: observedAt, FreshUntil: freshUntil, RetryAt: &retryAt,
		CustomerCode: "provider_rate_limited", CreatedAt: observedAt, UpdatedAt: observedAt,
	})
	if err != nil {
		t.Fatalf("UpsertMonotonic() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestCapabilityProviderHealthReadAndClearRequireExactTenantProviderScope(t *testing.T) {
	repository, mock, closeDatabase := newCapabilityProviderHealthSQLMockRepository(t)
	defer closeDatabase()
	observedAt := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	freshUntil := observedAt.Add(24 * time.Hour)
	mock.ExpectQuery(`SELECT business_id, provider_key, status, observed_at, fresh_until, retry_at, customer_code, created_at, updated_at[\s\S]*WHERE business_id = \$1 AND provider_key = \$2`).
		WithArgs("business-a", "gst_provider").
		WillReturnRows(sqlmock.NewRows([]string{
			"business_id", "provider_key", "status", "observed_at", "fresh_until", "retry_at", "customer_code", "created_at", "updated_at",
		}).AddRow("business-a", "gst_provider", "healthy", observedAt, freshUntil, nil, "", observedAt, observedAt))

	snapshot, err := repository.Get(context.Background(), "business-a", "gst_provider")
	if err != nil || snapshot.BusinessID != "business-a" || snapshot.ProviderKey != "gst_provider" {
		t.Fatalf("Get() = %#v, %v", snapshot, err)
	}
	mock.ExpectExec(`DELETE FROM capability_provider_health_snapshots WHERE business_id = \$1 AND provider_key = \$2`).
		WithArgs("business-a", "gst_provider").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repository.Clear(context.Background(), "business-a", "gst_provider"); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}

	_, err = repository.Get(context.Background(), "business-b", "razorpay_payments")
	if !errors.Is(err, interfaces.ErrCapabilityProviderHealthScope) {
		t.Fatalf("unrecognized scoped read error = %v", err)
	}
}

func TestCapabilityProviderHealthUpsertRejectsFreeformCustomerCodeBeforeSQL(t *testing.T) {
	repository, mock, closeDatabase := newCapabilityProviderHealthSQLMockRepository(t)
	defer closeDatabase()
	observedAt := time.Date(2026, 9, 1, 20, 15, 0, 0, time.UTC)

	err := repository.UpsertMonotonic(context.Background(), &models.CapabilityProviderHealthSnapshot{
		BusinessID: "business-a", ProviderKey: "gst_provider", Status: "unavailable",
		ObservedAt: observedAt, FreshUntil: observedAt.Add(time.Hour),
		CustomerCode: "raw account credential detail", CreatedAt: observedAt, UpdatedAt: observedAt,
	})

	if err == nil || err.Error() != "capability provider health classification is invalid" {
		t.Fatalf("UpsertMonotonic() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected SQL: %v", err)
	}
}

func newCapabilityProviderHealthSQLMockRepository(t *testing.T) (interfaces.CapabilityProviderHealthRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New(): %v", err)
	}
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("gorm.Open(): %v", err)
	}
	return NewCapabilityProviderHealthRepository(db), mock, func() { _ = sqlDB.Close() }
}
