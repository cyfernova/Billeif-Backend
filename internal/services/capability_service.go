package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/config"
)

type CapabilityState string

const (
	CapabilityStateAvailable              CapabilityState = "available"
	CapabilityStateSetupRequired          CapabilityState = "setup_required"
	CapabilityStateUpgradeRequired        CapabilityState = "upgrade_required"
	CapabilityStateQuotaExhausted         CapabilityState = "quota_exhausted"
	CapabilityStatePermissionDenied       CapabilityState = "permission_denied"
	CapabilityStateTemporarilyUnavailable CapabilityState = "temporarily_unavailable"
	CapabilityStateUnsupportedPlatform    CapabilityState = "unsupported_platform"
	CapabilityStateUnsupported            CapabilityState = "unsupported"
	CapabilityStateUnknown                CapabilityState = "unknown"
)

const (
	ReasonAvailable                = "available"
	ReasonCapabilityUnknown        = "capability_unknown"
	ReasonProviderNotConfigured    = "provider_not_configured"
	ReasonProviderHealthUnknown    = "provider_health_unknown"
	ReasonProviderHealthStale      = "provider_health_stale"
	ReasonProviderUnavailable      = "provider_temporarily_unavailable"
	ReasonProviderDegraded         = "provider_degraded"
	ReasonEntitlementRequired      = "entitlement_required"
	ReasonQuotaExhausted           = "quota_exhausted"
	ReasonPermissionRequired       = "permission_required"
	ReasonBusinessSetupRequired    = "business_setup_required"
	ReasonPlatformUnsupported      = "platform_unsupported"
	ReasonSavedPaymentsUnsupported = "saved_payment_methods_unsupported"
	ReasonBulkProcessorUnavailable = "bulk_import_processor_unavailable"
)

type CapabilityPlatform string

const (
	CapabilityPlatformWeb     CapabilityPlatform = "web"
	CapabilityPlatformIOS     CapabilityPlatform = "ios"
	CapabilityPlatformAndroid CapabilityPlatform = "android"
)

type CapabilityProductSupport struct {
	Supported bool `json:"supported"`
}

type CapabilityConfigurationFact struct {
	Required   bool `json:"required"`
	Configured bool `json:"configured"`
}

type CapabilityEntitlement struct {
	Required bool `json:"required"`
	Entitled bool `json:"entitled"`
}

type CapabilityPermission struct {
	Required bool   `json:"required"`
	Key      string `json:"key,omitempty"`
	Granted  bool   `json:"granted"`
}

type CapabilitySetup struct {
	Required bool `json:"required"`
	Complete bool `json:"complete"`
}

type CapabilityPlatformFact struct {
	Requested          CapabilityPlatform   `json:"requested"`
	Supported          bool                 `json:"supported"`
	SupportedPlatforms []CapabilityPlatform `json:"supported_platforms"`
}

type Capability struct {
	Key            CapabilityKey               `json:"key"`
	ProductSupport CapabilityProductSupport    `json:"product_support"`
	Configuration  CapabilityConfigurationFact `json:"configuration"`
	ProviderHealth CapabilityProviderHealth    `json:"provider_health"`
	Entitlement    CapabilityEntitlement       `json:"entitlement"`
	Quota          CapabilityQuota             `json:"quota"`
	Permission     CapabilityPermission        `json:"permission"`
	BusinessSetup  CapabilitySetup             `json:"business_setup"`
	Platform       CapabilityPlatformFact      `json:"platform"`
	Available      bool                        `json:"available"`
	State          CapabilityState             `json:"state"`
	ReasonCode     string                      `json:"reason_code"`
	SetupAction    string                      `json:"setup_action,omitempty"`
	RetryAt        *time.Time                  `json:"retry_at,omitempty"`
	Degradation    *CapabilityDegradation      `json:"degradation,omitempty"`
	EvaluatedAt    time.Time                   `json:"evaluated_at"`
}

type CapabilityList struct {
	BusinessID   string             `json:"business_id"`
	Platform     CapabilityPlatform `json:"platform"`
	EvaluatedAt  time.Time          `json:"evaluated_at"`
	Capabilities []Capability       `json:"capabilities"`
}

type CapabilityRequest struct {
	BusinessID           string
	UserID               string
	Platform             CapabilityPlatform
	Capability           CapabilityKey
	IntegrationAccountID string
	GSTServiceType       string
}

type CapabilityGuard interface {
	Require(ctx context.Context, request CapabilityRequest) error
}

func requireCapability(ctx context.Context, guard CapabilityGuard, request CapabilityRequest) error {
	if guard == nil {
		return &CapabilityUnavailableError{
			Code: "capability_unavailable", Capability: request.Capability,
			State: CapabilityStateUnknown, ReasonCode: "capability_evaluation_failed",
		}
	}
	return guard.Require(ctx, request)
}

type CapabilityUnavailableError struct {
	Code        string          `json:"code"`
	Capability  CapabilityKey   `json:"capability"`
	State       CapabilityState `json:"state"`
	ReasonCode  string          `json:"reason_code"`
	SetupAction string          `json:"setup_action,omitempty"`
	RetryAt     *time.Time      `json:"retry_at,omitempty"`
	cause       error
}

func (e *CapabilityUnavailableError) Error() string {
	return fmt.Sprintf("capability %s is unavailable: %s", e.Capability, e.ReasonCode)
}

func (e *CapabilityUnavailableError) Unwrap() error { return e.cause }

type CapabilityBusinessSetup struct {
	BusinessExists     bool
	GST                bool
	WhatsApp           bool
	Email              bool
	Voice              bool
	StorefrontPayments bool
}

type CapabilityEntitlementResolver interface {
	InspectFeature(ctx context.Context, businessID, feature string) (FeatureAccess, error)
}

type CapabilityBusinessSetupReader interface {
	ReadCapabilityBusinessSetup(ctx context.Context, businessID string) (CapabilityBusinessSetup, error)
}

type CapabilityHealthScope string

const (
	CapabilityHealthScopeGlobal     CapabilityHealthScope = "global"
	CapabilityHealthScopeBusiness   CapabilityHealthScope = "business"
	CapabilityHealthScopeUnobserved CapabilityHealthScope = "unobserved"
)

type CapabilityServiceOptions struct {
	Configuration  config.CapabilityConfiguration
	Entitlements   CapabilityEntitlementResolver
	Permissions    PermissionChecker
	Setup          CapabilityBusinessSetupReader
	GlobalHealth   *CapabilityGlobalHealthCache
	BusinessHealth CapabilityBusinessHealthFactReader
	Now            func() time.Time
}

type CapabilityService struct {
	configuration  config.CapabilityConfiguration
	entitlements   CapabilityEntitlementResolver
	permissions    PermissionChecker
	setup          CapabilityBusinessSetupReader
	globalHealth   *CapabilityGlobalHealthCache
	businessHealth CapabilityBusinessHealthFactReader
	now            func() time.Time
}

type capabilityDefinition struct {
	Key               CapabilityKey
	Supported         bool
	UnsupportedReason string
	Configuration     func(config.CapabilityConfiguration) bool
	Feature           string
	Permission        string
	Platforms         []CapabilityPlatform
	Setup             func(CapabilityBusinessSetup) bool
	SetupAction       string
	HealthKey         CapabilityKey
	HealthScope       CapabilityHealthScope
}

var capabilityDefinitions = []capabilityDefinition{
	{Key: CapabilityRazorpay, Supported: true, Configuration: func(c config.CapabilityConfiguration) bool { return c.Razorpay }, Permission: PermissionPaymentsManage, Platforms: allCapabilityPlatforms(), Setup: businessExistsSetup, SetupAction: "contact_support", HealthKey: CapabilityRazorpay, HealthScope: CapabilityHealthScopeGlobal},
	{Key: CapabilityGSTProvider, Supported: true, Configuration: func(c config.CapabilityConfiguration) bool { return c.GST }, Feature: FeatureGSTAPI, Permission: PermissionTaxIntegrationsManage, Platforms: allCapabilityPlatforms(), Setup: func(s CapabilityBusinessSetup) bool { return s.GST }, SetupAction: "configure_gst", HealthKey: CapabilityGSTProvider, HealthScope: CapabilityHealthScopeBusiness},
	{Key: CapabilityEInvoice, Supported: true, Configuration: func(c config.CapabilityConfiguration) bool { return c.GST }, Feature: FeatureEInvoice, Permission: PermissionDocumentsManage, Platforms: allCapabilityPlatforms(), Setup: func(s CapabilityBusinessSetup) bool { return s.GST }, SetupAction: "configure_gst", HealthKey: CapabilityGSTProvider, HealthScope: CapabilityHealthScopeBusiness},
	{Key: CapabilityEWayBill, Supported: true, Configuration: func(c config.CapabilityConfiguration) bool { return c.GST }, Feature: FeatureEWayBill, Permission: PermissionDocumentsManage, Platforms: allCapabilityPlatforms(), Setup: func(s CapabilityBusinessSetup) bool { return s.GST }, SetupAction: "configure_gst", HealthKey: CapabilityGSTProvider, HealthScope: CapabilityHealthScopeBusiness},
	{Key: CapabilityWhatsApp, Supported: true, Configuration: func(c config.CapabilityConfiguration) bool { return c.WhatsApp }, Feature: FeatureWhatsAppNotifications, Permission: PermissionNotificationsManage, Platforms: allCapabilityPlatforms(), Setup: func(s CapabilityBusinessSetup) bool { return s.WhatsApp }, SetupAction: "configure_whatsapp", HealthKey: CapabilityWhatsApp, HealthScope: CapabilityHealthScopeUnobserved},
	{Key: CapabilityEmail, Supported: true, Configuration: func(c config.CapabilityConfiguration) bool { return c.Email }, Permission: PermissionNotificationsManage, Platforms: allCapabilityPlatforms(), Setup: func(s CapabilityBusinessSetup) bool { return s.Email }, SetupAction: "configure_email", HealthKey: CapabilityEmail, HealthScope: CapabilityHealthScopeUnobserved},
	{Key: CapabilityS3Uploads, Supported: true, Configuration: func(c config.CapabilityConfiguration) bool { return c.S3Uploads }, Feature: FeatureDriveStorageMB, Permission: PermissionDriveManage, Platforms: allCapabilityPlatforms(), Setup: businessExistsSetup, SetupAction: "contact_support", HealthKey: CapabilityS3Uploads, HealthScope: CapabilityHealthScopeUnobserved},
	{Key: CapabilityVoice, Supported: true, Configuration: func(c config.CapabilityConfiguration) bool { return c.Voice }, Permission: PermissionVoiceUse, Platforms: []CapabilityPlatform{CapabilityPlatformIOS, CapabilityPlatformAndroid}, Setup: func(s CapabilityBusinessSetup) bool { return s.Voice }, SetupAction: "enable_voice", HealthKey: CapabilityVoice, HealthScope: CapabilityHealthScopeUnobserved},
	{Key: CapabilityAI, Supported: true, Configuration: func(c config.CapabilityConfiguration) bool { return c.AI }, Permission: PermissionAgentsView, Platforms: allCapabilityPlatforms(), Setup: businessExistsSetup, SetupAction: "contact_support", HealthKey: CapabilityAI, HealthScope: CapabilityHealthScopeGlobal},
	{Key: CapabilityStorefrontPayments, Supported: true, Configuration: func(c config.CapabilityConfiguration) bool { return c.Razorpay }, Feature: FeatureOnlineStore, Permission: PermissionStorefrontManage, Platforms: allCapabilityPlatforms(), Setup: func(s CapabilityBusinessSetup) bool { return s.StorefrontPayments }, SetupAction: "enable_storefront_payments", HealthKey: CapabilityRazorpay, HealthScope: CapabilityHealthScopeGlobal},
	{Key: CapabilityReportExports, Supported: true, Feature: FeatureExportDocuments, Permission: PermissionReportsExport, Platforms: allCapabilityPlatforms(), Setup: businessExistsSetup, SetupAction: "complete_business_setup", HealthScope: CapabilityHealthScopeUnobserved},
	{Key: CapabilityBulkImports, Supported: false, UnsupportedReason: ReasonBulkProcessorUnavailable, Platforms: allCapabilityPlatforms(), HealthScope: CapabilityHealthScopeUnobserved},
	{Key: CapabilitySavedPayments, Supported: false, UnsupportedReason: ReasonSavedPaymentsUnsupported, Permission: PermissionPaymentsManage, Platforms: allCapabilityPlatforms(), HealthScope: CapabilityHealthScopeUnobserved},
}

func NewCapabilityService(opts CapabilityServiceOptions) *CapabilityService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &CapabilityService{
		configuration:  opts.Configuration,
		entitlements:   opts.Entitlements,
		permissions:    opts.Permissions,
		setup:          opts.Setup,
		globalHealth:   opts.GlobalHealth,
		businessHealth: opts.BusinessHealth,
		now:            opts.Now,
	}
}

func (s *CapabilityService) Evaluate(ctx context.Context, request CapabilityRequest) (Capability, error) {
	if strings.TrimSpace(request.BusinessID) == "" || strings.TrimSpace(request.UserID) == "" {
		return Capability{}, fmt.Errorf("business and user are required")
	}
	request.Platform = normalizeCapabilityPlatform(request.Platform)
	definition, ok := capabilityDefinitionByKey(request.Capability)
	if !ok {
		return unknownCapability(request, s.now().UTC()), nil
	}
	setup, err := s.readSetup(ctx, request.BusinessID)
	if err != nil {
		return Capability{}, fmt.Errorf("read capability business setup: %w", err)
	}
	return s.evaluateDefinition(ctx, request, definition, setup)
}

// Require is the reusable service-layer preflight for governed mutations.
// Entitlement quota reservations remain authoritative at the mutation's
// transaction boundary; this check never reserves capacity.
func (s *CapabilityService) Require(ctx context.Context, request CapabilityRequest) error {
	result, err := s.Evaluate(ctx, request)
	if err != nil {
		return &CapabilityUnavailableError{
			Code: "capability_unavailable", Capability: request.Capability,
			State: CapabilityStateUnknown, ReasonCode: "capability_evaluation_failed", cause: err,
		}
	}
	if result.Available {
		return nil
	}
	retryAt := cloneCapabilityTime(result.RetryAt)
	return &CapabilityUnavailableError{
		Code: "capability_unavailable", Capability: result.Key, State: result.State,
		ReasonCode: result.ReasonCode, SetupAction: result.SetupAction, RetryAt: retryAt,
	}
}

func cloneCapabilityTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

var _ error = (*CapabilityUnavailableError)(nil)

func (s *CapabilityService) List(ctx context.Context, businessID, userID string, platform CapabilityPlatform) (CapabilityList, error) {
	if strings.TrimSpace(businessID) == "" || strings.TrimSpace(userID) == "" {
		return CapabilityList{}, fmt.Errorf("business and user are required")
	}
	platform = normalizeCapabilityPlatform(platform)
	setup, err := s.readSetup(ctx, businessID)
	if err != nil {
		return CapabilityList{}, fmt.Errorf("read capability business setup: %w", err)
	}
	evaluatedAt := s.now().UTC()
	result := CapabilityList{BusinessID: businessID, Platform: platform, EvaluatedAt: evaluatedAt, Capabilities: make([]Capability, 0, len(capabilityDefinitions))}
	for _, definition := range capabilityDefinitions {
		capability, err := s.evaluateDefinition(ctx, CapabilityRequest{BusinessID: businessID, UserID: userID, Platform: platform, Capability: definition.Key}, definition, setup)
		if err != nil {
			return CapabilityList{}, err
		}
		capability.EvaluatedAt = evaluatedAt
		result.Capabilities = append(result.Capabilities, capability)
	}
	return result, nil
}

func (s *CapabilityService) evaluateDefinition(ctx context.Context, request CapabilityRequest, definition capabilityDefinition, setup CapabilityBusinessSetup) (Capability, error) {
	result := Capability{
		Key: definition.Key, ProductSupport: CapabilityProductSupport{Supported: definition.Supported},
		Configuration:  CapabilityConfigurationFact{Required: definition.Configuration != nil},
		ProviderHealth: CapabilityProviderHealth{Status: CapabilityProviderUnknown},
		Entitlement:    CapabilityEntitlement{Required: definition.Feature != "", Entitled: definition.Feature == ""},
		Quota:          CapabilityQuota{Available: definition.Feature == ""},
		Permission:     CapabilityPermission{Required: definition.Permission != "", Key: definition.Permission, Granted: definition.Permission == ""},
		BusinessSetup:  CapabilitySetup{Required: definition.Setup != nil, Complete: definition.Setup == nil},
		Platform:       CapabilityPlatformFact{Requested: request.Platform, Supported: platformSupported(definition.Platforms, request.Platform), SupportedPlatforms: append([]CapabilityPlatform(nil), definition.Platforms...)},
		State:          CapabilityStateUnknown, ReasonCode: ReasonCapabilityUnknown, EvaluatedAt: s.now().UTC(),
	}
	if definition.Configuration != nil {
		result.Configuration.Configured = definition.Configuration(s.configuration)
	} else {
		result.Configuration.Configured = true
	}
	if definition.Feature != "" {
		if s.entitlements == nil {
			result.Entitlement = CapabilityEntitlement{Required: true}
			result.Quota = CapabilityQuota{Available: false}
		} else {
			access, err := s.entitlements.InspectFeature(ctx, request.BusinessID, definition.Feature)
			if err != nil {
				return Capability{}, fmt.Errorf("inspect capability entitlement: %w", err)
			}
			result.Entitlement = CapabilityEntitlement{Required: access.Required, Entitled: access.Entitled}
			result.Quota = access.Quota
		}
	}
	if definition.Permission != "" {
		result.Permission.Granted = s.permissions != nil && s.permissions.UserHasPermission(ctx, request.UserID, request.BusinessID, definition.Permission)
	}
	if definition.Setup != nil {
		result.BusinessSetup.Complete = definition.Setup(setup)
	}
	if definition.HealthScope == CapabilityHealthScopeGlobal && s.globalHealth != nil {
		if health, ok := s.globalHealth.CustomerFact(definition.HealthKey); ok {
			result.ProviderHealth = health
			result.RetryAt = health.RetryAt
			result.Degradation = health.Degradation
		}
	} else if definition.HealthScope == CapabilityHealthScopeBusiness && s.businessHealth != nil {
		var (
			health CapabilityProviderHealth
			ok     bool
			err    error
		)
		if bound, supported := s.businessHealth.(CapabilityBusinessHealthAccountFactReader); supported && strings.TrimSpace(request.IntegrationAccountID) != "" {
			health, ok, err = bound.CustomerFactForAccount(ctx, request.BusinessID, definition.HealthKey, request.IntegrationAccountID, request.GSTServiceType)
		} else {
			health, ok, err = s.businessHealth.CustomerFact(ctx, request.BusinessID, definition.HealthKey)
		}
		if err != nil {
			return Capability{}, fmt.Errorf("read capability provider health: %w", err)
		}
		if ok {
			result.ProviderHealth = health
			result.RetryAt = health.RetryAt
			result.Degradation = health.Degradation
		}
	}

	switch {
	case !definition.Supported:
		result.State = CapabilityStateUnsupported
		result.ReasonCode = definition.UnsupportedReason
	case !result.Platform.Supported:
		result.State = CapabilityStateUnsupportedPlatform
		result.ReasonCode = ReasonPlatformUnsupported
	case result.Configuration.Required && !result.Configuration.Configured:
		result.State = CapabilityStateSetupRequired
		result.ReasonCode = ReasonProviderNotConfigured
		result.SetupAction = definition.SetupAction
	case result.Entitlement.Required && !result.Entitlement.Entitled:
		result.State = CapabilityStateUpgradeRequired
		result.ReasonCode = ReasonEntitlementRequired
		result.SetupAction = "upgrade_subscription"
	case result.Quota.Limited && !result.Quota.Available:
		result.State = CapabilityStateQuotaExhausted
		result.ReasonCode = ReasonQuotaExhausted
		result.SetupAction = "upgrade_subscription"
	case result.Permission.Required && !result.Permission.Granted:
		result.State = CapabilityStatePermissionDenied
		result.ReasonCode = ReasonPermissionRequired
		result.SetupAction = "request_permission"
	case result.BusinessSetup.Required && !result.BusinessSetup.Complete:
		result.State = CapabilityStateSetupRequired
		result.ReasonCode = ReasonBusinessSetupRequired
		result.SetupAction = definition.SetupAction
	case definition.HealthKey != "" && result.ProviderHealth.Status == CapabilityProviderUnknown:
		result.State = CapabilityStateUnknown
		result.ReasonCode = ReasonProviderHealthUnknown
		if definition.HealthScope == CapabilityHealthScopeBusiness {
			result.SetupAction = "validate_gst_integration"
		}
	case result.ProviderHealth.Stale:
		result.State = CapabilityStateTemporarilyUnavailable
		result.ReasonCode = ReasonProviderHealthStale
		if definition.HealthScope == CapabilityHealthScopeBusiness {
			result.SetupAction = "validate_gst_integration"
		}
	case result.ProviderHealth.Status == CapabilityProviderUnavailable:
		result.State = CapabilityStateTemporarilyUnavailable
		result.ReasonCode = ReasonProviderUnavailable
		if definition.HealthScope == CapabilityHealthScopeBusiness {
			result.SetupAction = "validate_gst_integration"
		}
	case result.ProviderHealth.Status == CapabilityProviderDegraded:
		result.Available = true
		result.State = CapabilityStateAvailable
		result.ReasonCode = ReasonProviderDegraded
	default:
		result.Available = true
		result.State = CapabilityStateAvailable
		result.ReasonCode = ReasonAvailable
	}
	return result, nil
}

func (s *CapabilityService) readSetup(ctx context.Context, businessID string) (CapabilityBusinessSetup, error) {
	if s.setup == nil {
		return CapabilityBusinessSetup{}, nil
	}
	return s.setup.ReadCapabilityBusinessSetup(ctx, businessID)
}

func capabilityDefinitionByKey(key CapabilityKey) (capabilityDefinition, bool) {
	for _, definition := range capabilityDefinitions {
		if definition.Key == key {
			return definition, true
		}
	}
	return capabilityDefinition{}, false
}

func unknownCapability(request CapabilityRequest, evaluatedAt time.Time) Capability {
	return Capability{
		Key: request.Capability, ProductSupport: CapabilityProductSupport{Supported: false},
		ProviderHealth: CapabilityProviderHealth{Status: CapabilityProviderUnknown},
		Entitlement:    CapabilityEntitlement{}, Quota: CapabilityQuota{},
		Platform: CapabilityPlatformFact{Requested: request.Platform, Supported: false, SupportedPlatforms: []CapabilityPlatform{}},
		State:    CapabilityStateUnknown, ReasonCode: ReasonCapabilityUnknown, EvaluatedAt: evaluatedAt,
	}
}

func allCapabilityPlatforms() []CapabilityPlatform {
	return []CapabilityPlatform{CapabilityPlatformWeb, CapabilityPlatformIOS, CapabilityPlatformAndroid}
}

func businessExistsSetup(setup CapabilityBusinessSetup) bool { return setup.BusinessExists }

func platformSupported(platforms []CapabilityPlatform, requested CapabilityPlatform) bool {
	for _, platform := range platforms {
		if platform == requested {
			return true
		}
	}
	return false
}

func normalizeCapabilityPlatform(platform CapabilityPlatform) CapabilityPlatform {
	if platform == "" {
		return CapabilityPlatformWeb
	}
	return CapabilityPlatform(strings.ToLower(strings.TrimSpace(string(platform))))
}
