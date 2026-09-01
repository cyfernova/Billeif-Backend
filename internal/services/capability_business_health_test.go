package services

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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
	mu        sync.Mutex
	snapshots map[string]models.CapabilityProviderHealthSnapshot
	reads     []string
}

func newMemoryCapabilityProviderHealthRepository() *memoryCapabilityProviderHealthRepository {
	return &memoryCapabilityProviderHealthRepository{snapshots: make(map[string]models.CapabilityProviderHealthSnapshot)}
}

func (r *memoryCapabilityProviderHealthRepository) Get(_ context.Context, businessID, providerKey string) (*models.CapabilityProviderHealthSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads = append(r.reads, businessID+"\x00"+providerKey)
	snapshot, found := r.snapshots[businessID+"\x00"+providerKey]
	if !found {
		return nil, interfaces.ErrCapabilityProviderHealthNotFound
	}
	return &snapshot, nil
}

func (r *memoryCapabilityProviderHealthRepository) UpsertMonotonic(_ context.Context, snapshot *models.CapabilityProviderHealthSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := snapshot.BusinessID + "\x00" + snapshot.ProviderKey
	current, found := r.snapshots[key]
	if !found || snapshot.ObservedAt.After(current.ObservedAt) {
		r.snapshots[key] = *snapshot
	}
	return nil
}

func (r *memoryCapabilityProviderHealthRepository) Clear(_ context.Context, businessID, providerKey string) error {
	r.mu.Lock()
	delete(r.snapshots, businessID+"\x00"+providerKey)
	r.mu.Unlock()
	return nil
}

func TestGSTProviderHealthRecorderPersistsOnlySanitizedTenantSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 1, 20, 30, 0, 0, time.UTC)
	repository := newMemoryCapabilityProviderHealthRepository()
	recorder := NewGSTProviderHealthRecorder(repository, GSTProviderHealthRecorderOptions{
		Now:      func() time.Time { return now },
		FreshFor: 24 * time.Hour,
	})

	err := recorder.RecordGSTOutcome(context.Background(), "business-a", CapabilityProviderOutcome{
		HTTPStatus: 429,
		Err:        errors.New("raw credential account-id provider response"),
	})
	require.NoError(t, err)
	snapshot := repository.snapshots["business-a\x00gst_provider"]
	require.Equal(t, "business-a", snapshot.BusinessID)
	require.Equal(t, "gst_provider", snapshot.ProviderKey)
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

func TestCapabilityBusinessHealthReaderIsTenantScopedSanitizedAndDurable(t *testing.T) {
	now := time.Date(2026, 9, 1, 21, 0, 0, 0, time.UTC)
	repository := newMemoryCapabilityProviderHealthRepository()
	repository.snapshots["business-a\x00gst_provider"] = models.CapabilityProviderHealthSnapshot{
		BusinessID: "business-a", ProviderKey: "gst_provider", Status: "healthy",
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
	repository.snapshots["business-a\x00gst_provider"] = models.CapabilityProviderHealthSnapshot{
		BusinessID: "business-a", ProviderKey: "gst_provider", Status: "healthy",
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
		encrypted_credentials TEXT, credential_hint TEXT, status TEXT NOT NULL,
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
	repository.snapshots[businessID+"\x00gst_provider"] = models.CapabilityProviderHealthSnapshot{
		BusinessID: businessID, ProviderKey: "gst_provider", Status: "healthy",
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
