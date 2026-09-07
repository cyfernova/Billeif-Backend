package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCapabilityProviderHealthPostgresCredentialMutationWinsObservationRace(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not configured; skipping capability health PostgreSQL integration")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("MIGRATION_TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	admin, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open disposable PostgreSQL database: %v", err)
	}
	if err := ensureEmptyDisposableDatabase(admin); err != nil {
		t.Fatal(err)
	}
	schema := "capability_health_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schema)).Error; err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schema)).Error; err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	database, err := gorm.Open(gormpostgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open isolated PostgreSQL schema: %v", err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatalf("access PostgreSQL pool: %v", err)
	}
	sqlDatabase.SetMaxOpenConns(4)
	if err := database.Exec(`
CREATE TABLE business_profiles (
	id UUID PRIMARY KEY,
	deleted_at TIMESTAMPTZ
);
CREATE TABLE gst_integration_accounts (
	id UUID PRIMARY KEY,
	business_id UUID NOT NULL REFERENCES business_profiles(id),
	provider VARCHAR(80) NOT NULL,
	service_type VARCHAR(40) NOT NULL,
	gsp_name VARCHAR(120),
	portal_username VARCHAR(160),
	encrypted_credentials TEXT,
	credential_hint VARCHAR(255),
	credential_revision BIGINT NOT NULL DEFAULT 1 CHECK (credential_revision > 0),
	status VARCHAR(30) NOT NULL,
	last_validated_at TIMESTAMPTZ,
	last_error TEXT,
	metadata JSONB NOT NULL DEFAULT '{}',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	deleted_at TIMESTAMPTZ,
	UNIQUE (id, business_id, credential_revision)
);
CREATE TABLE capability_provider_health_snapshots (
	business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
	provider_key VARCHAR(64) NOT NULL,
	integration_account_id UUID NOT NULL,
	credential_revision BIGINT NOT NULL,
	observation_revision BIGINT NOT NULL DEFAULT 1,
	status VARCHAR(32) NOT NULL,
	observed_at TIMESTAMPTZ NOT NULL,
	fresh_until TIMESTAMPTZ NOT NULL,
	retry_at TIMESTAMPTZ,
	customer_code VARCHAR(64) NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (business_id, provider_key),
	FOREIGN KEY (integration_account_id, business_id, credential_revision)
		REFERENCES gst_integration_accounts(id, business_id, credential_revision)
		ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED
);`).Error; err != nil {
		t.Fatalf("create capability health integration tables: %v", err)
	}

	businessID, accountID := uuid.NewString(), uuid.NewString()
	seededAt := time.Date(2026, 9, 1, 20, 20, 0, 0, time.UTC)
	if err := database.Exec(`INSERT INTO business_profiles (id) VALUES (?)`, businessID).Error; err != nil {
		t.Fatalf("seed business: %v", err)
	}
	if err := database.Exec(`
		INSERT INTO gst_integration_accounts (
			id, business_id, provider, service_type, encrypted_credentials,
			credential_revision, status, metadata, created_at, updated_at
		) VALUES (?, ?, 'configured', 'einvoice', 'encrypted-fixture', 1, 'pending', '{}', ?, ?)
	`, accountID, businessID, seededAt, seededAt).Error; err != nil {
		t.Fatalf("seed GST account: %v", err)
	}

	repository := NewCapabilityProviderHealthRepository(database)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		_, recordErr := repository.RecordRevisionBound(context.Background(), &models.CapabilityProviderHealthSnapshot{
			BusinessID: businessID, ProviderKey: "gst_provider", IntegrationAccountID: accountID,
			CredentialRevision: 1, Status: "healthy", ObservedAt: seededAt,
			FreshUntil: seededAt.Add(time.Hour), CreatedAt: seededAt, UpdatedAt: seededAt,
		})
		if recordErr != nil && !errors.Is(recordErr, interfaces.ErrCapabilityProviderHealthCredentialRevisionStale) {
			errs <- recordErr
			return
		}
		errs <- nil
	}()
	go func() {
		defer wait.Done()
		<-start
		errs <- repository.SaveGSTIntegrationAccountAndInvalidate(context.Background(), &models.GSTIntegrationAccount{
			ID: accountID, BusinessID: businessID, Provider: "configured", ServiceType: "einvoice",
			EncryptedCredentials: "new-encrypted-fixture", Status: "pending", Metadata: `{}`, UpdatedAt: seededAt.Add(time.Second),
		}, 1)
	}()
	close(start)
	wait.Wait()
	close(errs)
	for raceErr := range errs {
		if raceErr != nil {
			t.Fatalf("credential/observation race: %v", raceErr)
		}
	}

	if _, err := repository.Get(context.Background(), businessID, "gst_provider"); !errors.Is(err, interfaces.ErrCapabilityProviderHealthNotFound) {
		t.Fatalf("health after credential race error = %v, want not found", err)
	}
	var revision int64
	if err := database.Raw(`SELECT credential_revision FROM gst_integration_accounts WHERE id = ?`, accountID).Scan(&revision).Error; err != nil {
		t.Fatalf("read credential revision: %v", err)
	}
	if revision != 2 {
		t.Fatalf("credential revision after race = %d, want 2", revision)
	}
}
