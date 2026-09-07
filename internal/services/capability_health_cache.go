package services

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrCapabilityGlobalHealthScope = errors.New("capability is not a global provider-health key")

type CapabilityKey string

const (
	CapabilityRazorpay           CapabilityKey = "razorpay_payments"
	CapabilityGSTProvider        CapabilityKey = "gst_provider"
	CapabilityEInvoice           CapabilityKey = "e_invoice"
	CapabilityEWayBill           CapabilityKey = "e_way_bill"
	CapabilityWhatsApp           CapabilityKey = "whatsapp_messaging"
	CapabilityEmail              CapabilityKey = "email_delivery"
	CapabilityS3Uploads          CapabilityKey = "s3_uploads"
	CapabilityVoice              CapabilityKey = "voice"
	CapabilityAI                 CapabilityKey = "ai"
	CapabilityStorefrontPayments CapabilityKey = "storefront_payments"
	CapabilityReportExports      CapabilityKey = "report_exports"
	CapabilityBulkImports        CapabilityKey = "bulk_imports"
	CapabilitySavedPayments      CapabilityKey = "saved_payment_methods"
)

type CapabilityProviderStatus string

const (
	CapabilityProviderUnknown     CapabilityProviderStatus = "unknown"
	CapabilityProviderHealthy     CapabilityProviderStatus = "healthy"
	CapabilityProviderDegraded    CapabilityProviderStatus = "degraded"
	CapabilityProviderUnavailable CapabilityProviderStatus = "unavailable"
)

type CapabilityDegradation struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type CapabilityProviderHealth struct {
	Status      CapabilityProviderStatus `json:"status"`
	ObservedAt  *time.Time               `json:"observed_at,omitempty"`
	Stale       bool                     `json:"stale"`
	RetryAt     *time.Time               `json:"-"`
	Degradation *CapabilityDegradation   `json:"-"`
}

// CapabilityHealthObservation contains only bounded customer-safe
// classifications. It never retains a provider error or tenant fact.
type CapabilityHealthObservation struct {
	Status       CapabilityProviderStatus
	ObservedAt   time.Time
	RetryAt      *time.Time
	CustomerCode string
}

// CapabilityGlobalHealthCache contains only provider-network observations
// whose scope is global by definition. Its API has no tenant identifier, and
// the accepted key set is fixed, so its size cannot follow tenant growth.
type CapabilityGlobalHealthCache struct {
	mu           sync.RWMutex
	observations map[CapabilityKey]CapabilityHealthObservation
	maxAge       time.Duration
	now          func() time.Time
}

type CapabilityGlobalHealthCacheOptions struct {
	MaxAge time.Duration
	Now    func() time.Time
}

func NewCapabilityGlobalHealthCache(opts CapabilityGlobalHealthCacheOptions) *CapabilityGlobalHealthCache {
	if opts.MaxAge <= 0 {
		opts.MaxAge = 5 * time.Minute
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &CapabilityGlobalHealthCache{
		observations: make(map[CapabilityKey]CapabilityHealthObservation, 2),
		maxAge:       opts.MaxAge,
		now:          opts.Now,
	}
}

func (c *CapabilityGlobalHealthCache) Record(capability CapabilityKey, observation CapabilityHealthObservation) error {
	if c == nil || !isGlobalCapabilityHealthKey(capability) {
		return ErrCapabilityGlobalHealthScope
	}
	if err := validateCapabilityHealthObservation(observation); err != nil {
		return err
	}
	observation.RetryAt = cloneCapabilityTime(observation.RetryAt)
	c.mu.Lock()
	current, found := c.observations[capability]
	if !found || !observation.ObservedAt.Before(current.ObservedAt) {
		c.observations[capability] = observation
	}
	c.mu.Unlock()
	return nil
}

func (c *CapabilityGlobalHealthCache) CustomerFact(capability CapabilityKey) (CapabilityProviderHealth, bool) {
	observation, found := c.OperatorObservation(capability)
	if !found {
		return CapabilityProviderHealth{Status: CapabilityProviderUnknown}, false
	}
	return capabilityCustomerHealth(observation, c.now().UTC(), c.maxAge), true
}

func (c *CapabilityGlobalHealthCache) OperatorObservation(capability CapabilityKey) (CapabilityHealthObservation, bool) {
	if c == nil || !isGlobalCapabilityHealthKey(capability) {
		return CapabilityHealthObservation{}, false
	}
	c.mu.RLock()
	observation, found := c.observations[capability]
	c.mu.RUnlock()
	observation.RetryAt = cloneCapabilityTime(observation.RetryAt)
	return observation, found
}

func isGlobalCapabilityHealthKey(capability CapabilityKey) bool {
	return capability == CapabilityRazorpay || capability == CapabilityAI
}

func validateCapabilityHealthObservation(observation CapabilityHealthObservation) error {
	if observation.ObservedAt.IsZero() {
		return fmt.Errorf("provider observation time is required")
	}
	switch {
	case observation.Status == CapabilityProviderHealthy && observation.CustomerCode == "":
		return nil
	case observation.Status == CapabilityProviderDegraded &&
		(observation.CustomerCode == "provider_degraded" || observation.CustomerCode == "provider_rate_limited"):
		return nil
	case observation.Status == CapabilityProviderUnavailable && observation.CustomerCode == "provider_unavailable":
		return nil
	}
	return fmt.Errorf("invalid provider health classification")
}

func capabilityCustomerHealth(observation CapabilityHealthObservation, now time.Time, maxAge time.Duration) CapabilityProviderHealth {
	observedAt := observation.ObservedAt.UTC()
	fact := CapabilityProviderHealth{
		Status:     observation.Status,
		ObservedAt: &observedAt,
		Stale:      now.Sub(observedAt) > maxAge,
	}
	if observation.RetryAt != nil {
		retryAt := observation.RetryAt.UTC()
		fact.RetryAt = &retryAt
	}
	if observation.Status != CapabilityProviderHealthy {
		code, message := safeCapabilityDegradation(observation.Status, observation.CustomerCode)
		fact.Degradation = &CapabilityDegradation{Code: code, Message: message}
	}
	return fact
}

func safeCapabilityDegradation(status CapabilityProviderStatus, requestedCode string) (string, string) {
	switch requestedCode {
	case "provider_degraded":
		return "provider_degraded", "Provider service is degraded."
	case "provider_unavailable":
		return "provider_unavailable", "Provider service is temporarily unavailable."
	case "provider_rate_limited":
		return "provider_rate_limited", "Provider service is temporarily rate limited."
	}
	if status == CapabilityProviderDegraded {
		return "provider_degraded", "Provider service is degraded."
	}
	return "provider_unavailable", "Provider service is temporarily unavailable."
}
