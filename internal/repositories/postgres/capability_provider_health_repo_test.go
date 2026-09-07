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

func TestCapabilityProviderHealthRecordIsCredentialBoundAndDatabaseOrdered(t *testing.T) {
	repository, mock, closeDatabase := newCapabilityProviderHealthSQLMockRepository(t)
	defer closeDatabase()
	observedAt := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	freshUntil := observedAt.Add(24 * time.Hour)
	retryAt := observedAt.Add(time.Minute)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT credential_revision[\s\S]*FROM gst_integration_accounts[\s\S]*FOR UPDATE`).
		WithArgs("account-a", "business-a").
		WillReturnRows(sqlmock.NewRows([]string{"credential_revision"}).AddRow(int64(3)))
	mock.ExpectQuery(`INSERT INTO capability_provider_health_snapshots[\s\S]*ON CONFLICT \(business_id, provider_key\) DO UPDATE SET[\s\S]*observation_revision = capability_provider_health_snapshots\.observation_revision \+ 1[\s\S]*RETURNING observation_revision`).
		WithArgs("business-a", "gst_provider", "account-a", int64(3), "degraded", observedAt, freshUntil, &retryAt, "provider_rate_limited", observedAt, observedAt).
		WillReturnRows(sqlmock.NewRows([]string{"observation_revision"}).AddRow(int64(8)))
	mock.ExpectCommit()

	revision, err := repository.RecordRevisionBound(context.Background(), &models.CapabilityProviderHealthSnapshot{
		BusinessID: "business-a", ProviderKey: "gst_provider", IntegrationAccountID: "account-a",
		CredentialRevision: 3, Status: "degraded",
		ObservedAt: observedAt, FreshUntil: freshUntil, RetryAt: &retryAt,
		CustomerCode: "provider_rate_limited", CreatedAt: observedAt, UpdatedAt: observedAt,
	})
	if err != nil {
		t.Fatalf("RecordRevisionBound() error = %v", err)
	}
	if revision != 8 {
		t.Fatalf("RecordRevisionBound() revision = %d, want 8", revision)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestCapabilityProviderHealthRecordRejectsStaleCredentialRevision(t *testing.T) {
	repository, mock, closeDatabase := newCapabilityProviderHealthSQLMockRepository(t)
	defer closeDatabase()
	observedAt := time.Date(2026, 9, 1, 20, 5, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT credential_revision[\s\S]*FROM gst_integration_accounts[\s\S]*FOR UPDATE`).
		WithArgs("account-a", "business-a").
		WillReturnRows(sqlmock.NewRows([]string{"credential_revision"}).AddRow(int64(4)))
	mock.ExpectRollback()

	_, err := repository.RecordRevisionBound(context.Background(), &models.CapabilityProviderHealthSnapshot{
		BusinessID: "business-a", ProviderKey: "gst_provider", IntegrationAccountID: "account-a",
		CredentialRevision: 3, Status: "healthy", ObservedAt: observedAt,
		FreshUntil: observedAt.Add(time.Hour), CreatedAt: observedAt, UpdatedAt: observedAt,
	})
	if !errors.Is(err, interfaces.ErrCapabilityProviderHealthCredentialRevisionStale) {
		t.Fatalf("RecordRevisionBound() error = %v, want stale revision", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestCapabilityProviderHealthRecordSurfacesNonAppliedWrite(t *testing.T) {
	repository, mock, closeDatabase := newCapabilityProviderHealthSQLMockRepository(t)
	defer closeDatabase()
	observedAt := time.Date(2026, 9, 1, 20, 10, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT credential_revision[\s\S]*FROM gst_integration_accounts[\s\S]*FOR UPDATE`).
		WithArgs("account-a", "business-a").
		WillReturnRows(sqlmock.NewRows([]string{"credential_revision"}).AddRow(int64(1)))
	mock.ExpectQuery(`INSERT INTO capability_provider_health_snapshots[\s\S]*RETURNING observation_revision`).
		WillReturnRows(sqlmock.NewRows([]string{"observation_revision"}))
	mock.ExpectRollback()

	_, err := repository.RecordRevisionBound(context.Background(), &models.CapabilityProviderHealthSnapshot{
		BusinessID: "business-a", ProviderKey: "gst_provider", IntegrationAccountID: "account-a",
		CredentialRevision: 1, Status: "healthy", ObservedAt: observedAt,
		FreshUntil: observedAt.Add(time.Hour), CreatedAt: observedAt, UpdatedAt: observedAt,
	})
	if !errors.Is(err, interfaces.ErrCapabilityProviderHealthObservationNotApplied) {
		t.Fatalf("RecordRevisionBound() error = %v, want explicit non-applied error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestGSTIntegrationAccountCredentialChangeLocksBusinessAndInvalidatesHealthAtomically(t *testing.T) {
	repository, mock, closeDatabase := newCapabilityProviderHealthSQLMockRepository(t)
	defer closeDatabase()
	now := time.Date(2026, 9, 1, 20, 12, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM business_profiles[\s\S]*FOR UPDATE`).
		WithArgs("business-a").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("business-a"))
	mock.ExpectQuery(`SELECT id, credential_revision[\s\S]*FROM gst_integration_accounts[\s\S]*FOR UPDATE`).
		WithArgs("business-a").
		WillReturnRows(sqlmock.NewRows([]string{"id", "credential_revision"}).AddRow("account-a", int64(5)))
	mock.ExpectExec(`UPDATE gst_integration_accounts\s+SET credential_revision = credential_revision \+ 1\s+WHERE business_id = \$1`).
		WithArgs("business-a").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE gst_integration_accounts[\s\S]*encrypted_credentials = \$5[\s\S]*WHERE id = \$10 AND business_id = \$11 AND credential_revision = \$12`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM capability_provider_health_snapshots[\s\S]*WHERE business_id = \$1 AND provider_key = \$2`).
		WithArgs("business-a", "gst_provider").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	account := &models.GSTIntegrationAccount{
		ID: "account-a", BusinessID: "business-a", Provider: "configured", ServiceType: "einvoice",
		EncryptedCredentials: "encrypted-fixture", CredentialHint: "fixture", Status: "pending",
		Metadata: `{}`, UpdatedAt: now,
	}

	err := repository.SaveGSTIntegrationAccountAndInvalidate(context.Background(), account, 5)

	if err != nil {
		t.Fatalf("SaveGSTIntegrationAccountAndInvalidate() error = %v", err)
	}
	if account.CredentialRevision != 6 {
		t.Fatalf("saved credential revision = %d, want 6", account.CredentialRevision)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestCapabilityProviderHealthValidationStateAndObservationCommitTogether(t *testing.T) {
	repository, mock, closeDatabase := newCapabilityProviderHealthSQLMockRepository(t)
	defer closeDatabase()
	now := time.Date(2026, 9, 1, 20, 14, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT credential_revision[\s\S]*FROM gst_integration_accounts[\s\S]*FOR UPDATE`).
		WithArgs("account-a", "business-a").
		WillReturnRows(sqlmock.NewRows([]string{"credential_revision"}).AddRow(int64(2)))
	mock.ExpectQuery(`INSERT INTO capability_provider_health_snapshots[\s\S]*RETURNING observation_revision`).
		WillReturnRows(sqlmock.NewRows([]string{"observation_revision"}).AddRow(int64(3)))
	mock.ExpectExec(`UPDATE gst_integration_accounts[\s\S]*SET status = \$1, last_validated_at = \$2, last_error = \$3[\s\S]*credential_revision = \$7`).
		WithArgs("succeeded", &now, "", now, "account-a", "business-a", int64(2)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	revision, err := repository.RecordValidationRevisionBound(context.Background(), &models.CapabilityProviderHealthSnapshot{
		BusinessID: "business-a", ProviderKey: "gst_provider", IntegrationAccountID: "account-a",
		CredentialRevision: 2, Status: "healthy", ObservedAt: now, FreshUntil: now.Add(time.Hour),
		CreatedAt: now, UpdatedAt: now,
	}, interfaces.GSTIntegrationValidationState{Status: "succeeded", LastValidatedAt: &now})

	if err != nil || revision != 3 {
		t.Fatalf("RecordValidationRevisionBound() = %d, %v", revision, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestCapabilityProviderHealthReadRequiresExactTenantProviderScope(t *testing.T) {
	repository, mock, closeDatabase := newCapabilityProviderHealthSQLMockRepository(t)
	defer closeDatabase()
	observedAt := time.Date(2026, 9, 1, 20, 0, 0, 0, time.UTC)
	freshUntil := observedAt.Add(24 * time.Hour)
	mock.ExpectQuery(`SELECT h\.business_id, h\.provider_key, h\.integration_account_id,[\s\S]*JOIN gst_integration_accounts AS a[\s\S]*a\.credential_revision = h\.credential_revision[\s\S]*WHERE h\.business_id = \$1 AND h\.provider_key = \$2`).
		WithArgs("business-a", "gst_provider").
		WillReturnRows(sqlmock.NewRows([]string{
			"business_id", "provider_key", "integration_account_id", "credential_revision", "observation_revision",
			"status", "observed_at", "fresh_until", "retry_at", "customer_code", "created_at", "updated_at",
		}).AddRow("business-a", "gst_provider", "account-a", int64(1), int64(2), "healthy", observedAt, freshUntil, nil, "", observedAt, observedAt))

	snapshot, err := repository.Get(context.Background(), "business-a", "gst_provider")
	if err != nil || snapshot.BusinessID != "business-a" || snapshot.ProviderKey != "gst_provider" {
		t.Fatalf("Get() = %#v, %v", snapshot, err)
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

	_, err := repository.RecordRevisionBound(context.Background(), &models.CapabilityProviderHealthSnapshot{
		BusinessID: "business-a", ProviderKey: "gst_provider", IntegrationAccountID: "account-a",
		CredentialRevision: 1, Status: "unavailable",
		ObservedAt: observedAt, FreshUntil: observedAt.Add(time.Hour),
		CustomerCode: "raw account credential detail", CreatedAt: observedAt, UpdatedAt: observedAt,
	})

	if err == nil || err.Error() != "capability provider health classification is invalid" {
		t.Fatalf("RecordRevisionBound() error = %v", err)
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
