package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"invoice-backend/internal/config"

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
		{name: "health degraded", configured: true, setup: true, health: &CapabilityHealthObservation{Status: CapabilityProviderDegraded, ObservedAt: now, CustomerCode: "provider_degraded", OperatorDetail: "raw secret provider trace"}, wantState: CapabilityStateAvailable, wantReason: ReasonProviderDegraded, available: true},
		{name: "healthy", configured: true, setup: true, health: &CapabilityHealthObservation{Status: CapabilityProviderHealthy, ObservedAt: now}, wantState: CapabilityStateAvailable, wantReason: ReasonAvailable, available: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			health := NewCapabilityHealthCache(CapabilityHealthCacheOptions{MaxAge: 5 * time.Minute, Now: func() time.Time { return now }})
			if tt.health != nil {
				require.NoError(t, health.Record("biz-1", CapabilityEmail, *tt.health))
			}
			service := NewCapabilityService(CapabilityServiceOptions{
				Configuration: config.CapabilityConfiguration{Email: tt.configured},
				Permissions:   staticCapabilityPermissions{PermissionNotificationsManage: true},
				Setup:         staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true, Email: tt.setup}},
				Health:        health, Now: func() time.Time { return now },
			})
			result, err := service.Evaluate(context.Background(), CapabilityRequest{BusinessID: "biz-1", UserID: "user-1", Platform: CapabilityPlatformWeb, Capability: CapabilityEmail})
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
	staleHealth := NewCapabilityHealthCache(CapabilityHealthCacheOptions{MaxAge: time.Minute, Now: func() time.Time { return now }})
	require.NoError(t, staleHealth.Record("biz-1", CapabilityEmail, CapabilityHealthObservation{
		Status: CapabilityProviderHealthy, ObservedAt: now.Add(-2 * time.Minute),
	}))

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
				Configuration: config.CapabilityConfiguration{Email: true},
				Permissions:   staticCapabilityPermissions{PermissionNotificationsManage: true},
				Setup:         staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true, Email: true}},
				Health:        staleHealth, Now: func() time.Time { return now },
			}),
			request: CapabilityRequest{BusinessID: "biz-1", UserID: "user-1", Capability: CapabilityEmail},
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
	health := NewCapabilityHealthCache(CapabilityHealthCacheOptions{Now: func() time.Time { return now }})
	retryAt := now.Add(time.Minute)
	require.NoError(t, health.Record("biz-1", CapabilityEInvoice, CapabilityHealthObservation{
		Status: CapabilityProviderUnavailable, ObservedAt: now, RetryAt: &retryAt,
		CustomerCode: "provider_unavailable", OperatorDetail: "secret arn and raw provider error",
	}))

	service := NewCapabilityService(CapabilityServiceOptions{
		Configuration: config.CapabilityConfiguration{GST: true},
		Entitlements: staticCapabilityEntitlements{
			FeatureEInvoice: {Required: true, Entitled: true, Quota: CapabilityQuota{Limited: true, Limit: 100, Used: 20, Remaining: 80, Available: true}},
		},
		Permissions: staticCapabilityPermissions{PermissionDocumentsManage: true},
		Setup:       staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true, GST: true}},
		Health:      health,
		Now:         func() time.Time { return now },
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
	health := NewCapabilityHealthCache(CapabilityHealthCacheOptions{Now: func() time.Time { return now }})
	for _, capability := range []CapabilityKey{CapabilityRazorpay, CapabilityReportExports} {
		require.NoError(t, health.Record("biz-1", capability, CapabilityHealthObservation{Status: CapabilityProviderHealthy, ObservedAt: now}))
	}

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
		{name: "bulk processor unavailable", capability: CapabilityBulkImports, platform: CapabilityPlatformWeb, entitled: true, quota: CapabilityQuota{Available: true}, permissions: staticCapabilityPermissions{PermissionProductsManage: true}, wantState: CapabilityStateUnsupported, wantReason: ReasonBulkProcessorUnavailable},
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
				Setup:  staticCapabilitySetup{snapshot: CapabilityBusinessSetup{BusinessExists: true, GST: true, Voice: true}},
				Health: health, Now: func() time.Time { return now },
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
