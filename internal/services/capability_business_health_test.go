package services

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type memoryCapabilityProviderHealthRepository struct {
	mu               sync.Mutex
	db               *gorm.DB
	snapshots        map[string]models.CapabilityProviderHealthSnapshot
	accountRevisions map[string]int64
	reads            []string
}

func newMemoryCapabilityProviderHealthRepository() *memoryCapabilityProviderHealthRepository {
	return &memoryCapabilityProviderHealthRepository{
		snapshots:        make(map[string]models.CapabilityProviderHealthSnapshot),
		accountRevisions: make(map[string]int64),
	}
}

func (r *memoryCapabilityProviderHealthRepository) Get(_ context.Context, businessID, providerKey string) (*models.CapabilityProviderHealthSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads = append(r.reads, businessID+"\x00"+providerKey)
	snapshot, found := r.snapshots[businessID+"\x00"+providerKey]
	if !found {
		return nil, interfaces.ErrCapabilityProviderHealthNotFound
	}
	if current := r.accountRevisions[snapshot.IntegrationAccountID]; current != snapshot.CredentialRevision {
		return nil, interfaces.ErrCapabilityProviderHealthNotFound
	}
	return &snapshot, nil
}

func (r *memoryCapabilityProviderHealthRepository) RecordRevisionBound(ctx context.Context, snapshot *models.CapabilityProviderHealthSnapshot) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if r.accountRevisions[snapshot.IntegrationAccountID] != snapshot.CredentialRevision {
		return 0, interfaces.ErrCapabilityProviderHealthCredentialRevisionStale
	}
	key := snapshot.BusinessID + "\x00" + snapshot.ProviderKey
	current := r.snapshots[key]
	snapshot.ObservationRevision = current.ObservationRevision + 1
	if snapshot.ObservationRevision == 0 {
		snapshot.ObservationRevision = 1
	}
	r.snapshots[key] = *snapshot
	return snapshot.ObservationRevision, nil
}

func (r *memoryCapabilityProviderHealthRepository) RecordValidationRevisionBound(
	ctx context.Context,
	snapshot *models.CapabilityProviderHealthSnapshot,
	state interfaces.GSTIntegrationValidationState,
) (int64, error) {
	revision, err := r.RecordRevisionBound(ctx, snapshot)
	if err != nil {
		return 0, err
	}
	if r.db != nil {
		result := r.db.WithContext(ctx).Model(&models.GSTIntegrationAccount{}).
			Where("id = ? AND business_id = ? AND credential_revision = ? AND deleted_at IS NULL",
				snapshot.IntegrationAccountID, snapshot.BusinessID, snapshot.CredentialRevision).
			Updates(map[string]interface{}{
				"status":            state.Status,
				"last_validated_at": state.LastValidatedAt,
				"last_error":        state.LastError,
			})
		if result.Error != nil {
			return 0, result.Error
		}
		if result.RowsAffected != 1 {
			return 0, interfaces.ErrCapabilityProviderHealthObservationNotApplied
		}
	}
	return revision, nil
}

func (r *memoryCapabilityProviderHealthRepository) SaveGSTIntegrationAccountAndInvalidate(
	ctx context.Context,
	account *models.GSTIntegrationAccount,
	expectedCredentialRevision int64,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if expectedCredentialRevision > 0 && r.accountRevisions[account.ID] != expectedCredentialRevision {
		return interfaces.ErrCapabilityProviderHealthCredentialRevisionStale
	}
	for accountID := range r.accountRevisions {
		r.accountRevisions[accountID]++
	}
	if expectedCredentialRevision == 0 {
		account.CredentialRevision = 1
	} else {
		account.CredentialRevision = expectedCredentialRevision + 1
	}
	r.accountRevisions[account.ID] = account.CredentialRevision
	delete(r.snapshots, account.BusinessID+"\x00gst_provider")
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.GSTIntegrationAccount{}).
			Where("business_id = ? AND deleted_at IS NULL", account.BusinessID).
			UpdateColumn("credential_revision", gorm.Expr("credential_revision + 1")).Error; err != nil {
			return err
		}
		if expectedCredentialRevision == 0 {
			return tx.Create(account).Error
		}
		return tx.Model(&models.GSTIntegrationAccount{}).
			Where("id = ? AND business_id = ? AND credential_revision = ?", account.ID, account.BusinessID, account.CredentialRevision).
			Updates(map[string]interface{}{
				"provider":              account.Provider,
				"service_type":          account.ServiceType,
				"gsp_name":              account.GSPName,
				"portal_username":       account.PortalUsername,
				"encrypted_credentials": account.EncryptedCredentials,
				"credential_hint":       account.CredentialHint,
				"status":                account.Status,
				"last_validated_at":     nil,
				"last_error":            "",
				"metadata":              account.Metadata,
				"updated_at":            account.UpdatedAt,
			}).Error
	})
}

func TestGSTProviderHealthRecorderPersistsOnlySanitizedTenantSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 1, 20, 30, 0, 0, time.UTC)
	repository := newMemoryCapabilityProviderHealthRepository()
	repository.accountRevisions["account-a"] = 1
	recorder := NewGSTProviderHealthRecorder(repository, GSTProviderHealthRecorderOptions{
		Now:      func() time.Time { return now },
		FreshFor: 24 * time.Hour,
	})

	_, err := recorder.RecordGSTOutcome(context.Background(), "business-a", "account-a", 1, CapabilityProviderOutcome{
		HTTPStatus: 429,
		Err:        errors.New("raw credential account-id provider response"),
	})
	require.NoError(t, err)
	snapshot := repository.snapshots["business-a\x00gst_provider"]
	require.Equal(t, "business-a", snapshot.BusinessID)
	require.Equal(t, "gst_provider", snapshot.ProviderKey)
	require.Equal(t, "account-a", snapshot.IntegrationAccountID)
	require.EqualValues(t, 1, snapshot.CredentialRevision)
	require.EqualValues(t, 1, snapshot.ObservationRevision)
	require.Equal(t, "degraded", snapshot.Status)
	require.Equal(t, "provider_rate_limited", snapshot.CustomerCode)
	require.Equal(t, now, snapshot.ObservedAt)
	require.Equal(t, now.Add(24*time.Hour), snapshot.FreshUntil)
	require.NotNil(t, snapshot.RetryAt)
	serialized := fmt.Sprintf("%#v", snapshot)
	for _, forbidden := range []string{"raw credential", "account-id", "provider response"} {
		require.NotContains(t, serialized, forbidden)
	}
}

func TestGSTProviderHealthRecorderAdvancesEqualTimestampCompletions(t *testing.T) {
	now := time.Date(2026, 9, 1, 20, 45, 0, 0, time.UTC)
	repository := newMemoryCapabilityProviderHealthRepository()
	repository.accountRevisions["account-a"] = 7
	recorder := NewGSTProviderHealthRecorder(repository, GSTProviderHealthRecorderOptions{Now: func() time.Time { return now }})

	first, err := recorder.RecordGSTOutcome(context.Background(), "business-a", "account-a", 7, CapabilityProviderOutcome{})
	require.NoError(t, err)
	second, err := recorder.RecordGSTOutcome(context.Background(), "business-a", "account-a", 7, CapabilityProviderOutcome{HTTPStatus: http.StatusTooManyRequests})
	require.NoError(t, err)

	require.EqualValues(t, 1, first)
	require.EqualValues(t, 2, second)
	snapshot := repository.snapshots["business-a\x00gst_provider"]
	require.Equal(t, "degraded", snapshot.Status)
	require.EqualValues(t, 2, snapshot.ObservationRevision)
}

func TestCapabilityBusinessHealthReaderIsTenantScopedSanitizedAndDurable(t *testing.T) {
	now := time.Date(2026, 9, 1, 21, 0, 0, 0, time.UTC)
	repository := newMemoryCapabilityProviderHealthRepository()
	repository.accountRevisions["account-a"] = 1
	repository.snapshots["business-a\x00gst_provider"] = models.CapabilityProviderHealthSnapshot{
		BusinessID: "business-a", ProviderKey: "gst_provider", IntegrationAccountID: "account-a",
		CredentialRevision: 1, ObservationRevision: 1, Status: "healthy",
		ObservedAt: now, FreshUntil: now.Add(24 * time.Hour),
	}
	reader := NewCapabilityBusinessHealthReader(repository, func() time.Time { return now })

	fact, found, err := reader.CustomerFact(context.Background(), "business-a", CapabilityGSTProvider)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, CapabilityProviderHealthy, fact.Status)
	_, found, err = reader.CustomerFact(context.Background(), "business-b", CapabilityGSTProvider)
	require.NoError(t, err)
	require.False(t, found)
	_, found, err = reader.CustomerFact(context.Background(), "business-a", CapabilityRazorpay)
	require.NoError(t, err)
	require.False(t, found, "global health must never be read from tenant snapshots")
	require.Equal(t, []string{
		"business-a\x00gst_provider", "business-b\x00gst_provider",
	}, repository.reads)
}

func TestCapabilityServiceUsesTenantGSTSnapshotAndExposesValidationActionForUnknown(t *testing.T) {
	now := time.Date(2026, 9, 1, 21, 30, 0, 0, time.UTC)
	repository := newMemoryCapabilityProviderHealthRepository()
	repository.accountRevisions["account-a"] = 1
	repository.snapshots["business-a\x00gst_provider"] = models.CapabilityProviderHealthSnapshot{
		BusinessID: "business-a", ProviderKey: "gst_provider", IntegrationAccountID: "account-a",
		CredentialRevision: 1, ObservationRevision: 1, Status: "healthy",
		ObservedAt: now, FreshUntil: now.Add(24 * time.Hour),
	}
	service := NewCapabilityService(CapabilityServiceOptions{
		Configuration: config.CapabilityConfiguration{GST: true},
		Entitlements: staticCapabilityEntitlements{
			FeatureEInvoice: {Required: true, Entitled: true, Quota: CapabilityQuota{Available: true}},
		},
		Permissions:    staticCapabilityPermissions{PermissionDocumentsManage: true},
		Setup:          staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true, GST: true}},
		BusinessHealth: NewCapabilityBusinessHealthReader(repository, func() time.Time { return now }),
		Now:            func() time.Time { return now },
	})

	available, err := service.Evaluate(context.Background(), CapabilityRequest{
		BusinessID: "business-a", UserID: "user-a", Capability: CapabilityEInvoice,
	})
	require.NoError(t, err)
	require.True(t, available.Available)

	unknown, err := service.Evaluate(context.Background(), CapabilityRequest{
		BusinessID: "business-b", UserID: "user-b", Capability: CapabilityEInvoice,
	})
	require.NoError(t, err)
	require.False(t, unknown.Available)
	require.Equal(t, CapabilityStateUnknown, unknown.State)
	require.Equal(t, ReasonProviderHealthUnknown, unknown.ReasonCode)
	require.Equal(t, "validate_gst_integration", unknown.SetupAction)
	raw := fmt.Sprintf("%#v", unknown)
	require.False(t, strings.Contains(raw, "business-a"))
}

func TestCapabilityServiceBindsGSTReadinessToSelectedIntegrationAccount(t *testing.T) {
	now := time.Date(2026, 9, 1, 21, 15, 0, 0, time.UTC)
	repository := newMemoryCapabilityProviderHealthRepository()
	repository.accountRevisions["account-a"] = 1
	repository.accountRevisions["account-b"] = 1
	repository.snapshots["business-a\x00gst_provider"] = models.CapabilityProviderHealthSnapshot{
		BusinessID: "business-a", ProviderKey: "gst_provider", IntegrationAccountID: "account-a",
		CredentialRevision: 1, ObservationRevision: 1, Status: "healthy",
		ObservedAt: now, FreshUntil: now.Add(24 * time.Hour),
	}
	service := NewCapabilityService(CapabilityServiceOptions{
		Configuration: config.CapabilityConfiguration{GST: true},
		Entitlements:  staticCapabilityEntitlements{FeatureEInvoice: {Required: true, Entitled: true, Quota: CapabilityQuota{Available: true}}},
		Permissions:   staticCapabilityPermissions{PermissionDocumentsManage: true},
		Setup:         staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true, GST: true}},
		BusinessHealth: NewCapabilityBusinessHealthReader(repository, func() time.Time {
			return now
		}),
		Now: func() time.Time { return now },
	})

	capability, err := service.Evaluate(context.Background(), CapabilityRequest{
		BusinessID: "business-a", UserID: "user-a", Platform: CapabilityPlatformWeb,
		Capability: CapabilityEInvoice, IntegrationAccountID: "account-b", GSTServiceType: "einvoice",
	})

	require.NoError(t, err)
	require.False(t, capability.Available)
	require.Equal(t, CapabilityStateUnknown, capability.State)
	require.Equal(t, ReasonProviderHealthUnknown, capability.ReasonCode)
	require.Equal(t, "validate_gst_integration", capability.SetupAction)
}

type validationOnlyGSTProvider struct {
	GSTProvider
	err   error
	calls int
}

func (p *validationOnlyGSTProvider) ValidateCredentials(context.Context, *GSTIntegrationAccountCredentials) error {
	p.calls++
	return p.err
}

func TestGSTIntegrationCredentialUpdateClearsOldHealthAndExplicitValidationSeedsNewSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE gst_integration_accounts (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, provider TEXT NOT NULL,
		service_type TEXT NOT NULL, gsp_name TEXT, portal_username TEXT,
		encrypted_credentials TEXT, credential_hint TEXT, credential_revision INTEGER NOT NULL DEFAULT 1, status TEXT NOT NULL,
		last_validated_at DATETIME, last_error TEXT, metadata TEXT,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	accountID := uuid.NewString()
	businessID := uuid.NewString()
	require.NoError(t, db.Create(&models.GSTIntegrationAccount{
		ID: accountID, BusinessID: businessID, Provider: "test", ServiceType: "einvoice",
		Status: "pending", Metadata: `{}`,
	}).Error)
	now := time.Date(2026, 9, 1, 22, 0, 0, 0, time.UTC)
	repository := newMemoryCapabilityProviderHealthRepository()
	repository.db = db
	repository.accountRevisions[accountID] = 1
	repository.snapshots[businessID+"\x00gst_provider"] = models.CapabilityProviderHealthSnapshot{
		BusinessID: businessID, ProviderKey: "gst_provider", IntegrationAccountID: accountID,
		CredentialRevision: 1, ObservationRevision: 1, Status: "healthy",
		ObservedAt: now.Add(-time.Hour), FreshUntil: now.Add(23 * time.Hour),
	}
	recorder := NewGSTProviderHealthRecorder(repository, GSTProviderHealthRecorderOptions{
		Now: func() time.Time { return now },
	})
	provider := &validationOnlyGSTProvider{}
	service := NewTaxComplianceService(&config.Config{
		Credentials: config.CredentialsConfig{EncryptionKey: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))},
	}, db, nil, nil, nil, nil, nil, nil, nil, logger.New()).
		WithGSTProviderHealthRecorder(recorder)
	service.provider = provider

	_, err = service.UpsertIntegrationAccount(context.Background(), businessID, accountID, UpsertGSTIntegrationAccountInput{
		Provider: "test", ServiceType: "einvoice",
		Credentials: GSTIntegrationAccountCredentials{APIKey: "fixture-key", APISecret: "fixture-secret"},
	})
	require.NoError(t, err)
	_, found := repository.snapshots[businessID+"\x00gst_provider"]
	require.False(t, found, "changed credentials must invalidate their prior health snapshot")

	validated, err := service.ValidateIntegrationAccount(context.Background(), businessID, accountID)
	require.NoError(t, err)
	require.Equal(t, models.GSTJobStatusSucceeded, validated.Status)
	require.Equal(t, 1, provider.calls)
	snapshot, found := repository.snapshots[businessID+"\x00gst_provider"]
	require.True(t, found)
	require.Equal(t, "healthy", snapshot.Status)
	require.Equal(t, now, snapshot.ObservedAt)

	provider.err = errors.New("raw GST account acct_123 credential response")
	now = now.Add(time.Minute)
	failed, err := service.ValidateIntegrationAccount(context.Background(), businessID, accountID)
	require.ErrorIs(t, err, ErrGSTCredentialValidationFailed)
	require.Equal(t, models.GSTJobStatusFailed, failed.Status)
	require.Equal(t, "GST integration credential validation failed", failed.LastError)
	require.NotContains(t, err.Error(), "acct_123")
	require.NotContains(t, failed.LastError, "credential response")
	snapshot = repository.snapshots[businessID+"\x00gst_provider"]
	require.Equal(t, "unavailable", snapshot.Status)
	require.Equal(t, "provider_unavailable", snapshot.CustomerCode)
	require.Equal(t, now, snapshot.ObservedAt)
}

func TestGSTIntegrationValidationWithoutConfiguredPathStaysUnknownWithoutProviderOrHealthWrite(t *testing.T) {
	var providerCalls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		providerCalls++
	}))
	defer server.Close()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE gst_integration_accounts (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, provider TEXT NOT NULL,
		service_type TEXT NOT NULL, gsp_name TEXT, portal_username TEXT,
		encrypted_credentials TEXT, credential_hint TEXT, credential_revision INTEGER NOT NULL DEFAULT 1, status TEXT NOT NULL,
		last_validated_at DATETIME, last_error TEXT, metadata TEXT,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	businessID := uuid.NewString()
	accountID := uuid.NewString()
	cfg := &config.Config{
		GST:         config.GSTConfig{BaseURL: server.URL, ValidatePath: "   "},
		Credentials: config.CredentialsConfig{EncryptionKey: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))},
	}
	repository := newMemoryCapabilityProviderHealthRepository()
	repository.db = db
	repository.accountRevisions[accountID] = 1
	service := NewTaxComplianceService(cfg, db, nil, nil, nil, nil, nil, nil, nil, logger.New()).
		WithGSTProviderHealthRecorder(NewGSTProviderHealthRecorder(repository, GSTProviderHealthRecorderOptions{}))
	encrypted, _, err := service.encryptIntegrationCredentials(context.Background(), GSTIntegrationAccountCredentials{APIKey: "fixture-key"})
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.GSTIntegrationAccount{
		ID: accountID, BusinessID: businessID, Provider: "configured", ServiceType: "einvoice",
		EncryptedCredentials: encrypted, Status: "pending", Metadata: `{}`,
	}).Error)

	account, err := service.ValidateIntegrationAccount(context.Background(), businessID, accountID)

	require.ErrorIs(t, err, ErrGSTCredentialValidationNotConfigured)
	require.Equal(t, "pending", account.Status)
	require.Zero(t, providerCalls)
	require.Empty(t, repository.snapshots, "unsupported validation must not create a healthy snapshot")
}

type blockingValidationGSTProvider struct {
	GSTProvider
	started chan struct{}
	release chan struct{}
}

func (p *blockingValidationGSTProvider) ValidateCredentials(context.Context, *GSTIntegrationAccountCredentials) error {
	close(p.started)
	<-p.release
	return nil
}

func TestGSTIntegrationValidationCannotRestoreHealthAfterCredentialRevisionChanges(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE gst_integration_accounts (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, provider TEXT NOT NULL,
		service_type TEXT NOT NULL, gsp_name TEXT, portal_username TEXT,
		encrypted_credentials TEXT, credential_hint TEXT, credential_revision INTEGER NOT NULL DEFAULT 1,
		status TEXT NOT NULL, last_validated_at DATETIME, last_error TEXT, metadata TEXT,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	businessID := uuid.NewString()
	accountID := uuid.NewString()
	repository := newMemoryCapabilityProviderHealthRepository()
	repository.db = db
	repository.accountRevisions[accountID] = 1
	recorder := NewGSTProviderHealthRecorder(repository, GSTProviderHealthRecorderOptions{})
	service := NewTaxComplianceService(&config.Config{
		Credentials: config.CredentialsConfig{EncryptionKey: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))},
	}, db, nil, nil, nil, nil, nil, nil, nil, logger.New()).WithGSTProviderHealthRecorder(recorder)
	encrypted, _, err := service.encryptIntegrationCredentials(context.Background(), GSTIntegrationAccountCredentials{APIKey: "old-fixture-key"})
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.GSTIntegrationAccount{
		ID: accountID, BusinessID: businessID, Provider: "test", ServiceType: "einvoice",
		EncryptedCredentials: encrypted, CredentialRevision: 1, Status: "pending", Metadata: `{}`,
	}).Error)
	provider := &blockingValidationGSTProvider{started: make(chan struct{}), release: make(chan struct{})}
	service.provider = provider

	validationResult := make(chan error, 1)
	go func() {
		_, validateErr := service.ValidateIntegrationAccount(context.Background(), businessID, accountID)
		validationResult <- validateErr
	}()
	<-provider.started

	updated, err := service.UpsertIntegrationAccount(context.Background(), businessID, accountID, UpsertGSTIntegrationAccountInput{
		Provider: "test", ServiceType: "einvoice",
		Credentials: GSTIntegrationAccountCredentials{APIKey: "new-fixture-key"},
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, updated.CredentialRevision)
	close(provider.release)

	require.ErrorIs(t, <-validationResult, interfaces.ErrCapabilityProviderHealthCredentialRevisionStale)
	require.Empty(t, repository.snapshots, "old-revision validation must not recreate health")
	var persisted models.GSTIntegrationAccount
	require.NoError(t, db.First(&persisted, "id = ?", accountID).Error)
	require.EqualValues(t, 2, persisted.CredentialRevision)
	require.Equal(t, "pending", persisted.Status)
}

func TestGSTExecutionCannotFallBackToDifferentGlobalCredentials(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE gst_integration_accounts (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, provider TEXT NOT NULL,
		service_type TEXT NOT NULL, gsp_name TEXT, portal_username TEXT,
		encrypted_credentials TEXT, credential_hint TEXT, credential_revision INTEGER NOT NULL DEFAULT 1,
		status TEXT NOT NULL, last_validated_at DATETIME, last_error TEXT, metadata TEXT,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	service := NewTaxComplianceService(&config.Config{GST: config.GSTConfig{
		Username: "global-user", Password: "global-password", ClientID: "global-client", ClientSecret: "global-secret",
	}}, db, nil, nil, nil, nil, nil, nil, nil, logger.New())

	account, credentials, err := service.resolveIntegrationAccount(
		context.Background(), uuid.NewString(), models.GSTOperationGenerateEInvoice,
	)

	require.ErrorIs(t, err, ErrGSTIntegrationAccountRequired)
	require.Nil(t, account)
	require.Empty(t, credentials)
}

func TestGSTIntegrationAccountMissingUsesStableServiceError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE gst_integration_accounts (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, provider TEXT NOT NULL,
		service_type TEXT NOT NULL, gsp_name TEXT, portal_username TEXT,
		encrypted_credentials TEXT, credential_hint TEXT, credential_revision INTEGER NOT NULL DEFAULT 1,
		status TEXT NOT NULL, last_validated_at DATETIME, last_error TEXT, metadata TEXT,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	service := NewTaxComplianceService(&config.Config{
		Credentials: config.CredentialsConfig{EncryptionKey: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))},
	}, db, nil, nil, nil, nil, nil, nil, nil, logger.New())
	businessID := uuid.NewString()
	accountID := uuid.NewString()

	_, upsertErr := service.UpsertIntegrationAccount(context.Background(), businessID, accountID, UpsertGSTIntegrationAccountInput{
		ServiceType: "einvoice", Credentials: GSTIntegrationAccountCredentials{APIKey: "fixture-key"},
	})
	_, validateErr := service.ValidateIntegrationAccount(context.Background(), businessID, accountID)

	require.ErrorIs(t, upsertErr, interfaces.ErrGSTIntegrationAccountNotFound)
	require.ErrorIs(t, validateErr, interfaces.ErrGSTIntegrationAccountNotFound)
}

func TestGSTCredentialMutationRequiresRevisionBoundHealthPersistence(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE gst_integration_accounts (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, provider TEXT NOT NULL,
		service_type TEXT NOT NULL, gsp_name TEXT, portal_username TEXT,
		encrypted_credentials TEXT, credential_hint TEXT, credential_revision INTEGER NOT NULL DEFAULT 1,
		status TEXT NOT NULL, last_validated_at DATETIME, last_error TEXT, metadata TEXT,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	businessID := uuid.NewString()
	accountID := uuid.NewString()
	require.NoError(t, db.Create(&models.GSTIntegrationAccount{
		ID: accountID, BusinessID: businessID, Provider: "test", ServiceType: "einvoice",
		CredentialRevision: 1, EncryptedCredentials: "old-encrypted-fixture", Status: "pending", Metadata: `{}`,
	}).Error)
	service := NewTaxComplianceService(&config.Config{
		Credentials: config.CredentialsConfig{EncryptionKey: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))},
	}, db, nil, nil, nil, nil, nil, nil, nil, logger.New())

	_, err = service.UpsertIntegrationAccount(context.Background(), businessID, accountID, UpsertGSTIntegrationAccountInput{
		Provider: "test", ServiceType: "einvoice",
		Credentials: GSTIntegrationAccountCredentials{APIKey: "new-fixture-key"},
	})

	require.ErrorIs(t, err, ErrGSTProviderHealthPersistenceNotConfigured)
	var persisted models.GSTIntegrationAccount
	require.NoError(t, db.First(&persisted, "id = ?", accountID).Error)
	require.EqualValues(t, 1, persisted.CredentialRevision)
	require.Equal(t, "old-encrypted-fixture", persisted.EncryptedCredentials)
}

func TestGSTValidationRequiresRevisionBoundHealthPersistence(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE gst_integration_accounts (
		id TEXT PRIMARY KEY, business_id TEXT NOT NULL, provider TEXT NOT NULL,
		service_type TEXT NOT NULL, gsp_name TEXT, portal_username TEXT,
		encrypted_credentials TEXT, credential_hint TEXT, credential_revision INTEGER NOT NULL DEFAULT 1,
		status TEXT NOT NULL, last_validated_at DATETIME, last_error TEXT, metadata TEXT,
		created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)
	businessID := uuid.NewString()
	accountID := uuid.NewString()
	service := NewTaxComplianceService(&config.Config{
		Credentials: config.CredentialsConfig{EncryptionKey: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))},
	}, db, nil, nil, nil, nil, nil, nil, nil, logger.New())
	provider := &validationOnlyGSTProvider{}
	service.provider = provider
	encrypted, _, err := service.encryptIntegrationCredentials(context.Background(), GSTIntegrationAccountCredentials{APIKey: "fixture-key"})
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.GSTIntegrationAccount{
		ID: accountID, BusinessID: businessID, Provider: "test", ServiceType: "einvoice",
		CredentialRevision: 1, EncryptedCredentials: encrypted, Status: "pending", Metadata: `{}`,
	}).Error)

	account, err := service.ValidateIntegrationAccount(context.Background(), businessID, accountID)

	require.ErrorIs(t, err, ErrGSTProviderHealthPersistenceNotConfigured)
	require.Equal(t, "pending", account.Status)
	require.Zero(t, provider.calls)
	var persisted models.GSTIntegrationAccount
	require.NoError(t, db.First(&persisted, "id = ?", accountID).Error)
	require.Equal(t, "pending", persisted.Status)
	require.Nil(t, persisted.LastValidatedAt)
}
