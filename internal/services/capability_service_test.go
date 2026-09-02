package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"

	"github.com/stretchr/testify/require"
)

func TestCapabilityServiceConfigurationHealthSetupAndDegradationStatesAreStable(t *testing.T) {
	now := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		configured bool
		setup      bool
		health     *CapabilityHealthObservation
		wantState  CapabilityState
		wantReason string
		available  bool
	}{
		{name: "not configured", wantState: CapabilityStateSetupRequired, wantReason: ReasonProviderNotConfigured},
		{name: "business setup missing", configured: true, setup: false, health: &CapabilityHealthObservation{Status: CapabilityProviderHealthy, ObservedAt: now}, wantState: CapabilityStateSetupRequired, wantReason: ReasonBusinessSetupRequired},
		{name: "health unknown", configured: true, setup: true, wantState: CapabilityStateUnknown, wantReason: ReasonProviderHealthUnknown},
		{name: "health stale", configured: true, setup: true, health: &CapabilityHealthObservation{Status: CapabilityProviderHealthy, ObservedAt: now.Add(-10 * time.Minute)}, wantState: CapabilityStateTemporarilyUnavailable, wantReason: ReasonProviderHealthStale},
		{name: "health degraded", configured: true, setup: true, health: &CapabilityHealthObservation{Status: CapabilityProviderDegraded, ObservedAt: now, CustomerCode: "provider_degraded"}, wantState: CapabilityStateAvailable, wantReason: ReasonProviderDegraded, available: true},
		{name: "healthy", configured: true, setup: true, health: &CapabilityHealthObservation{Status: CapabilityProviderHealthy, ObservedAt: now}, wantState: CapabilityStateAvailable, wantReason: ReasonAvailable, available: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			health := capabilityBusinessHealthForTest(now, "biz-1", tt.health, 5*time.Minute)
			service := NewCapabilityService(CapabilityServiceOptions{
				Configuration:  config.CapabilityConfiguration{GST: tt.configured},
				Entitlements:   staticCapabilityEntitlements{FeatureGSTAPI: {Required: true, Entitled: true, Quota: CapabilityQuota{Available: true}}},
				Permissions:    staticCapabilityPermissions{PermissionTaxIntegrationsManage: true},
				Setup:          staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true, GST: tt.setup}},
				BusinessHealth: health, Now: func() time.Time { return now },
			})
			result, err := service.Evaluate(context.Background(), CapabilityRequest{BusinessID: "biz-1", UserID: "user-1", Platform: CapabilityPlatformWeb, Capability: CapabilityGSTProvider})
			require.NoError(t, err)
			require.Equal(t, tt.wantState, result.State)
			require.Equal(t, tt.wantReason, result.ReasonCode)
			require.Equal(t, tt.available, result.Available)
			raw, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "raw secret provider trace")
		})
	}
}

func TestCapabilityServiceUnknownCapabilityFailsClosed(t *testing.T) {
	service := NewCapabilityService(CapabilityServiceOptions{Setup: staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true}}})
	result, err := service.Evaluate(context.Background(), CapabilityRequest{
		BusinessID: "biz-1", UserID: "user-1", Platform: CapabilityPlatformWeb, Capability: CapabilityKey("future_provider"),
	})
	require.NoError(t, err)
	require.False(t, result.Available)
	require.Equal(t, CapabilityStateUnknown, result.State)
	require.Equal(t, ReasonCapabilityUnknown, result.ReasonCode)
}

func TestGSTFeaturesShareTenantScopedProviderHealth(t *testing.T) {
	now := time.Date(2026, 9, 1, 16, 10, 0, 0, time.UTC)
	health := capabilityBusinessHealthForTest(now, "biz-1", &CapabilityHealthObservation{Status: CapabilityProviderHealthy, ObservedAt: now}, 24*time.Hour)
	service := NewCapabilityService(CapabilityServiceOptions{
		Configuration: config.CapabilityConfiguration{GST: true},
		Entitlements: staticCapabilityEntitlements{
			FeatureEInvoice: {Required: true, Entitled: true, Quota: CapabilityQuota{Available: true}},
			FeatureEWayBill: {Required: true, Entitled: true, Quota: CapabilityQuota{Available: true}},
		},
		Permissions:    staticCapabilityPermissions{PermissionDocumentsManage: true},
		Setup:          staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true, GST: true}},
		BusinessHealth: health, Now: func() time.Time { return now },
	})

	for _, capability := range []CapabilityKey{CapabilityEInvoice, CapabilityEWayBill} {
		result, err := service.Evaluate(context.Background(), CapabilityRequest{BusinessID: "biz-1", UserID: "user-1", Capability: capability})
		require.NoError(t, err)
		require.True(t, result.Available, "%s should use the shared GST provider fact", capability)
	}
	other, err := service.Evaluate(context.Background(), CapabilityRequest{BusinessID: "biz-2", UserID: "user-1", Capability: CapabilityEInvoice})
	require.NoError(t, err)
	require.Equal(t, CapabilityStateUnknown, other.State)
}

func TestCapabilityServiceRequireRejectsUnsupportedCapabilityWithTypedError(t *testing.T) {
	service := NewCapabilityService(CapabilityServiceOptions{
		Setup: staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true}},
	})

	err := service.Require(context.Background(), CapabilityRequest{
		BusinessID: "biz-1", UserID: "user-1", Platform: CapabilityPlatformWeb, Capability: CapabilitySavedPayments,
	})

	var unavailable *CapabilityUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, CapabilitySavedPayments, unavailable.Capability)
	require.Equal(t, CapabilityStateUnsupported, unavailable.State)
	require.Equal(t, ReasonSavedPaymentsUnsupported, unavailable.ReasonCode)
	require.Equal(t, "capability_unavailable", unavailable.Code)
}

func TestCapabilityServiceRequireFailsClosedForEveryUnavailableState(t *testing.T) {
	now := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)
	staleHealth := capabilityBusinessHealthForTest(now, "biz-1", &CapabilityHealthObservation{
		Status: CapabilityProviderHealthy, ObservedAt: now.Add(-2 * time.Minute),
	}, time.Minute)

	tests := []struct {
		name      string
		service   *CapabilityService
		request   CapabilityRequest
		wantState CapabilityState
	}{
		{
			name: "setup", wantState: CapabilityStateSetupRequired,
			service: NewCapabilityService(CapabilityServiceOptions{Setup: staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true}}}),
			request: CapabilityRequest{BusinessID: "biz-1", UserID: "user-1", Capability: CapabilityEmail},
		},
		{
			name: "upgrade", wantState: CapabilityStateUpgradeRequired,
			service: NewCapabilityService(CapabilityServiceOptions{
				Entitlements: staticCapabilityEntitlements{FeatureExportDocuments: {Required: true, Entitled: false}},
				Permissions:  staticCapabilityPermissions{PermissionReportsExport: true},
				Setup:        staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true}},
			}),
			request: CapabilityRequest{BusinessID: "biz-1", UserID: "user-1", Capability: CapabilityReportExports},
		},
		{
			name: "quota", wantState: CapabilityStateQuotaExhausted,
			service: NewCapabilityService(CapabilityServiceOptions{
				Configuration: config.CapabilityConfiguration{GST: true},
				Entitlements: staticCapabilityEntitlements{FeatureEInvoice: {
					Required: true, Entitled: true, Quota: CapabilityQuota{Limited: true, Limit: 1, Used: 1, Available: false},
				}},
				Permissions: staticCapabilityPermissions{PermissionDocumentsManage: true},
				Setup:       staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true, GST: true}},
			}),
			request: CapabilityRequest{BusinessID: "biz-1", UserID: "user-1", Capability: CapabilityEInvoice},
		},
		{
			name: "permission", wantState: CapabilityStatePermissionDenied,
			service: NewCapabilityService(CapabilityServiceOptions{
				Entitlements: staticCapabilityEntitlements{FeatureExportDocuments: {Required: true, Entitled: true, Quota: CapabilityQuota{Available: true}}},
				Permissions:  staticCapabilityPermissions{},
				Setup:        staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true}},
			}),
			request: CapabilityRequest{BusinessID: "biz-1", UserID: "user-1", Capability: CapabilityReportExports},
		},
		{
			name: "stale", wantState: CapabilityStateTemporarilyUnavailable,
			service: NewCapabilityService(CapabilityServiceOptions{
				Configuration:  config.CapabilityConfiguration{GST: true},
				Entitlements:   staticCapabilityEntitlements{FeatureGSTAPI: {Required: true, Entitled: true, Quota: CapabilityQuota{Available: true}}},
				Permissions:    staticCapabilityPermissions{PermissionTaxIntegrationsManage: true},
				Setup:          staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true, GST: true}},
				BusinessHealth: staleHealth, Now: func() time.Time { return now },
			}),
			request: CapabilityRequest{BusinessID: "biz-1", UserID: "user-1", Capability: CapabilityGSTProvider},
		},
		{
			name: "unsupported platform", wantState: CapabilityStateUnsupportedPlatform,
			service: NewCapabilityService(CapabilityServiceOptions{
				Configuration: config.CapabilityConfiguration{Voice: true},
				Permissions:   staticCapabilityPermissions{PermissionVoiceUse: true},
				Setup:         staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true, Voice: true}},
			}),
			request: CapabilityRequest{BusinessID: "biz-1", UserID: "user-1", Platform: CapabilityPlatformWeb, Capability: CapabilityVoice},
		},
		{
			name: "unsupported", wantState: CapabilityStateUnsupported,
			service: NewCapabilityService(CapabilityServiceOptions{Setup: staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true}}}),
			request: CapabilityRequest{BusinessID: "biz-1", UserID: "user-1", Capability: CapabilitySavedPayments},
		},
		{
			name: "unknown", wantState: CapabilityStateUnknown,
			service: NewCapabilityService(CapabilityServiceOptions{Setup: staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true}}}),
			request: CapabilityRequest{BusinessID: "biz-1", UserID: "user-1", Capability: CapabilityKey("future_capability")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.service.Require(context.Background(), test.request)
			var unavailable *CapabilityUnavailableError
			require.ErrorAs(t, err, &unavailable)
			require.Equal(t, test.wantState, unavailable.State)
		})
	}
}

func TestCapabilityServiceListCoversAuthoritativeCapabilityInventory(t *testing.T) {
	service := NewCapabilityService(CapabilityServiceOptions{
		Setup: staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true}},
	})
	result, err := service.List(context.Background(), "biz-1", "user-1", CapabilityPlatformWeb)
	require.NoError(t, err)
	keys := make([]CapabilityKey, 0, len(result.Capabilities))
	for _, capability := range result.Capabilities {
		keys = append(keys, capability.Key)
	}
	require.Equal(t, []CapabilityKey{
		CapabilityRazorpay, CapabilityGSTProvider, CapabilityEInvoice, CapabilityEWayBill,
		CapabilityWhatsApp, CapabilityEmail, CapabilityS3Uploads, CapabilityVoice, CapabilityAI,
		CapabilityStorefrontPayments, CapabilityReportExports, CapabilityBulkImports, CapabilitySavedPayments,
	}, keys)
}

func TestCapabilityQuotaSerializesZeroValuesExplicitly(t *testing.T) {
	raw, err := json.Marshal(CapabilityQuota{Limited: true, Available: false})
	require.NoError(t, err)
	require.JSONEq(t, `{"limited":true,"limit":0,"used":0,"remaining":0,"available":false}`, string(raw))
}

func TestCapabilityServiceSeparatesHealthEntitlementPermissionAndFinalState(t *testing.T) {
	now := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)
	retryAt := now.Add(time.Minute)
	health := capabilityBusinessHealthForTest(now, "biz-1", &CapabilityHealthObservation{
		Status: CapabilityProviderUnavailable, ObservedAt: now, RetryAt: &retryAt,
		CustomerCode: "provider_unavailable",
	}, 24*time.Hour)

	service := NewCapabilityService(CapabilityServiceOptions{
		Configuration: config.CapabilityConfiguration{GST: true},
		Entitlements: staticCapabilityEntitlements{
			FeatureEInvoice: {Required: true, Entitled: true, Quota: CapabilityQuota{Limited: true, Limit: 100, Used: 20, Remaining: 80, Available: true}},
		},
		Permissions:    staticCapabilityPermissions{PermissionDocumentsManage: true},
		Setup:          staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true, GST: true}},
		BusinessHealth: health,
		Now:            func() time.Time { return now },
	})

	result, err := service.Evaluate(context.Background(), CapabilityRequest{
		BusinessID: "biz-1", UserID: "user-1", Platform: CapabilityPlatformWeb, Capability: CapabilityEInvoice,
	})
	require.NoError(t, err)
	require.True(t, result.ProductSupport.Supported)
	require.True(t, result.Configuration.Configured)
	require.True(t, result.Entitlement.Entitled, "health failure must not revoke entitlement")
	require.True(t, result.Quota.Available)
	require.True(t, result.Permission.Granted)
	require.True(t, result.BusinessSetup.Complete)
	require.Equal(t, CapabilityProviderUnavailable, result.ProviderHealth.Status)
	require.Equal(t, CapabilityStateTemporarilyUnavailable, result.State)
	require.Equal(t, ReasonProviderUnavailable, result.ReasonCode)
	require.Equal(t, retryAt, *result.RetryAt)
	require.False(t, result.Available)
	require.NotContains(t, result.Degradation.Message, "secret")
}

func TestCapabilityServiceReturnsStableProductPlatformEntitlementQuotaAndPermissionStates(t *testing.T) {
	now := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)
	health := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{Now: func() time.Time { return now }})
	require.NoError(t, health.Record(CapabilityRazorpay, CapabilityHealthObservation{Status: CapabilityProviderHealthy, ObservedAt: now}))

	tests := []struct {
		name        string
		capability  CapabilityKey
		platform    CapabilityPlatform
		entitled    bool
		quota       CapabilityQuota
		permissions staticCapabilityPermissions
		wantState   CapabilityState
		wantReason  string
	}{
		{name: "saved methods unsupported", capability: CapabilitySavedPayments, platform: CapabilityPlatformWeb, entitled: true, quota: CapabilityQuota{Available: true}, permissions: staticCapabilityPermissions{PermissionPaymentsManage: true}, wantState: CapabilityStateUnsupported, wantReason: ReasonSavedPaymentsUnsupported},
		{name: "web voice unsupported platform", capability: CapabilityVoice, platform: CapabilityPlatformWeb, entitled: true, quota: CapabilityQuota{Available: true}, permissions: staticCapabilityPermissions{PermissionVoiceUse: true}, wantState: CapabilityStateUnsupportedPlatform, wantReason: ReasonPlatformUnsupported},
		{name: "bulk imports ready", capability: CapabilityBulkImports, platform: CapabilityPlatformWeb, entitled: true, quota: CapabilityQuota{Available: true}, permissions: staticCapabilityPermissions{PermissionProductsManage: true}, wantState: CapabilityStateAvailable, wantReason: ReasonAvailable},
		{name: "upgrade required", capability: CapabilityReportExports, platform: CapabilityPlatformWeb, entitled: false, quota: CapabilityQuota{}, permissions: staticCapabilityPermissions{PermissionReportsExport: true}, wantState: CapabilityStateUpgradeRequired, wantReason: ReasonEntitlementRequired},
		{name: "quota exhausted", capability: CapabilityEInvoice, platform: CapabilityPlatformWeb, entitled: true, quota: CapabilityQuota{Limited: true, Limit: 100, Used: 100, Available: false}, permissions: staticCapabilityPermissions{PermissionDocumentsManage: true}, wantState: CapabilityStateQuotaExhausted, wantReason: ReasonQuotaExhausted},
		{name: "permission denied", capability: CapabilityRazorpay, platform: CapabilityPlatformWeb, entitled: true, quota: CapabilityQuota{Available: true}, permissions: staticCapabilityPermissions{}, wantState: CapabilityStatePermissionDenied, wantReason: ReasonPermissionRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entitlements := staticCapabilityEntitlements{}
			for _, feature := range []string{FeatureEInvoice, FeatureEWayBill, FeatureGSTAPI, FeatureWhatsAppNotifications, FeatureDriveStorageMB, FeatureOnlineStore, FeatureExportDocuments} {
				entitlements[feature] = FeatureAccess{Required: true, Entitled: tt.entitled, Quota: tt.quota}
			}
			service := NewCapabilityService(CapabilityServiceOptions{
				Configuration: config.CapabilityConfiguration{Razorpay: true, GST: true, Voice: true},
				Entitlements:  entitlements, Permissions: tt.permissions,
				Setup:        staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true, GST: true, Voice: true}},
				GlobalHealth: health, Now: func() time.Time { return now },
			})
			result, err := service.Evaluate(context.Background(), CapabilityRequest{BusinessID: "biz-1", UserID: "user-1", Platform: tt.platform, Capability: tt.capability})
			require.NoError(t, err)
			require.Equal(t, tt.wantState, result.State)
			require.Equal(t, tt.wantReason, result.ReasonCode)
			if tt.capability == CapabilitySavedPayments {
				require.True(t, result.Permission.Required)
				require.Equal(t, PermissionPaymentsManage, result.Permission.Key)
				require.True(t, result.Permission.Granted)
			}
		})
	}
}

type staticCapabilityEntitlements map[string]FeatureAccess

func (s staticCapabilityEntitlements) InspectFeature(_ context.Context, _, feature string) (FeatureAccess, error) {
	if access, ok := s[feature]; ok {
		return access, nil
	}
	return FeatureAccess{Required: true, Entitled: true, Quota: CapabilityQuota{Available: true}}, nil
}

type staticCapabilityPermissions map[string]bool

func (s staticCapabilityPermissions) UserHasPermission(_ context.Context, _, _, permission string) bool {
	return s[permission]
}

type staticCapabilitySetup struct {
	snapshot CapabilityBusinessSetup
	err      error
}

func (s staticCapabilitySetup) ReadCapabilityBusinessSetup(context.Context, string) (CapabilityBusinessSetup, error) {
	return s.snapshot, s.err
}

func capabilityBusinessHealthForTest(
	now time.Time,
	businessID string,
	observation *CapabilityHealthObservation,
	freshFor time.Duration,
) *CapabilityBusinessHealthReader {
	repository := newMemoryCapabilityProviderHealthRepository()
	if observation != nil {
		repository.snapshots[businessID+"\x00gst_provider"] = models.CapabilityProviderHealthSnapshot{
			BusinessID: businessID, ProviderKey: "gst_provider", Status: string(observation.Status),
			ObservedAt: observation.ObservedAt, FreshUntil: observation.ObservedAt.Add(freshFor),
			RetryAt: cloneCapabilityTime(observation.RetryAt), CustomerCode: observation.CustomerCode,
		}
	}
	return NewCapabilityBusinessHealthReader(repository, func() time.Time { return now })
}
