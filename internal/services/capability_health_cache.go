package services

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

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

// CapabilityHealthObservation is retained only in the process-local cache.
// OperatorDetail must never be copied into a customer capability response.
type CapabilityHealthObservation struct {
	Status         CapabilityProviderStatus
	ObservedAt     time.Time
	RetryAt        *time.Time
	CustomerCode   string
	OperatorDetail string
}

type CapabilityHealthCacheOptions struct {
	MaxAge     time.Duration
	MaxEntries int
	Now        func() time.Time
}

type CapabilityHealthCache struct {
	mu           sync.RWMutex
	observations map[string]CapabilityHealthObservation
	maxAge       time.Duration
	maxEntries   int
	now          func() time.Time
}

func NewCapabilityHealthCache(opts CapabilityHealthCacheOptions) *CapabilityHealthCache {
	if opts.MaxAge <= 0 {
		opts.MaxAge = 5 * time.Minute
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = 4096
	}
	return &CapabilityHealthCache{
		observations: make(map[string]CapabilityHealthObservation),
		maxAge:       opts.MaxAge,
		maxEntries:   opts.MaxEntries,
		now:          opts.Now,
	}
}

func (c *CapabilityHealthCache) Record(businessID string, capability CapabilityKey, observation CapabilityHealthObservation) error {
	if c == nil || strings.TrimSpace(businessID) == "" || strings.TrimSpace(string(capability)) == "" {
		return fmt.Errorf("business and capability are required")
	}
	if observation.ObservedAt.IsZero() {
		return fmt.Errorf("provider observation time is required")
	}
	switch observation.Status {
	case CapabilityProviderHealthy, CapabilityProviderDegraded, CapabilityProviderUnavailable:
	default:
		return fmt.Errorf("invalid provider health status")
	}
	c.mu.Lock()
	key := capabilityHealthCacheKey(businessID, capability)
	if current, ok := c.observations[key]; ok {
		if !observation.ObservedAt.Before(current.ObservedAt) {
			c.observations[key] = observation
		}
		c.mu.Unlock()
		return nil
	}
	if len(c.observations) >= c.maxEntries {
		oldestKey := ""
		var oldestAt time.Time
		for candidateKey, candidate := range c.observations {
			if oldestKey == "" || candidate.ObservedAt.Before(oldestAt) {
				oldestKey = candidateKey
				oldestAt = candidate.ObservedAt
			}
		}
		delete(c.observations, oldestKey)
	}
	c.observations[key] = observation
	c.mu.Unlock()
	return nil
}

func (c *CapabilityHealthCache) CustomerFact(businessID string, capability CapabilityKey) (CapabilityProviderHealth, bool) {
	observation, ok := c.OperatorObservation(businessID, capability)
	if !ok {
		return CapabilityProviderHealth{Status: CapabilityProviderUnknown}, false
	}
	observedAt := observation.ObservedAt.UTC()
	fact := CapabilityProviderHealth{
		Status:     observation.Status,
		ObservedAt: &observedAt,
		Stale:      c.now().UTC().Sub(observedAt) > c.maxAge,
	}
	if observation.RetryAt != nil {
		retryAt := observation.RetryAt.UTC()
		fact.RetryAt = &retryAt
	}
	if observation.Status != CapabilityProviderHealthy {
		code, message := safeCapabilityDegradation(observation.Status, observation.CustomerCode)
		fact.Degradation = &CapabilityDegradation{Code: code, Message: message}
	}
	return fact, true
}

func (c *CapabilityHealthCache) OperatorObservation(businessID string, capability CapabilityKey) (CapabilityHealthObservation, bool) {
	if c == nil {
		return CapabilityHealthObservation{}, false
	}
	c.mu.RLock()
	observation, ok := c.observations[capabilityHealthCacheKey(businessID, capability)]
	c.mu.RUnlock()
	return observation, ok
}

func capabilityHealthCacheKey(businessID string, capability CapabilityKey) string {
	return strings.TrimSpace(businessID) + "\x00" + string(capability)
}

func safeCapabilityDegradation(status CapabilityProviderStatus, requestedCode string) (string, string) {
	switch strings.TrimSpace(requestedCode) {
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
