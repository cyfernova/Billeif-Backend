package postgres

import (
	"context"
	"errors"
	"strings"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"gorm.io/gorm"
)

const gstProviderHealthKey = "gst_provider"

type capabilityProviderHealthRepository struct {
	db *gorm.DB
}

func NewCapabilityProviderHealthRepository(db *gorm.DB) interfaces.CapabilityProviderHealthRepository {
	return &capabilityProviderHealthRepository{db: db}
}

func (r *capabilityProviderHealthRepository) Get(ctx context.Context, businessID, providerKey string) (*models.CapabilityProviderHealthSnapshot, error) {
	if err := validateCapabilityProviderHealthScope(businessID, providerKey); err != nil {
		return nil, err
	}
	var snapshot models.CapabilityProviderHealthSnapshot
	result := r.db.WithContext(ctx).Raw(`
		SELECT business_id, provider_key, status, observed_at, fresh_until,
			retry_at, customer_code, created_at, updated_at
		FROM capability_provider_health_snapshots
		WHERE business_id = ? AND provider_key = ?
	`, strings.TrimSpace(businessID), providerKey).Scan(&snapshot)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, interfaces.ErrCapabilityProviderHealthNotFound
	}
	return &snapshot, nil
}

func (r *capabilityProviderHealthRepository) UpsertMonotonic(ctx context.Context, snapshot *models.CapabilityProviderHealthSnapshot) error {
	if snapshot == nil {
		return errors.New("capability provider health snapshot is required")
	}
	if err := validateCapabilityProviderHealthScope(snapshot.BusinessID, snapshot.ProviderKey); err != nil {
		return err
	}
	if snapshot.ObservedAt.IsZero() || snapshot.FreshUntil.Before(snapshot.ObservedAt) {
		return errors.New("capability provider health timestamps are invalid")
	}
	switch snapshot.Status {
	case "healthy", "degraded", "unavailable":
	default:
		return errors.New("capability provider health status is invalid")
	}
	if !validCapabilityProviderHealthClassification(snapshot.Status, snapshot.CustomerCode) {
		return errors.New("capability provider health classification is invalid")
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO capability_provider_health_snapshots (
			business_id, provider_key, status, observed_at, fresh_until,
			retry_at, customer_code, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (business_id, provider_key) DO UPDATE SET
			status = EXCLUDED.status,
			observed_at = EXCLUDED.observed_at,
			fresh_until = EXCLUDED.fresh_until,
			retry_at = EXCLUDED.retry_at,
			customer_code = EXCLUDED.customer_code,
			updated_at = EXCLUDED.updated_at
		WHERE capability_provider_health_snapshots.observed_at < EXCLUDED.observed_at
	`, strings.TrimSpace(snapshot.BusinessID), snapshot.ProviderKey, snapshot.Status,
		snapshot.ObservedAt, snapshot.FreshUntil, snapshot.RetryAt, snapshot.CustomerCode,
		snapshot.CreatedAt, snapshot.UpdatedAt).Error
}

func validCapabilityProviderHealthClassification(status, customerCode string) bool {
	switch status {
	case "healthy":
		return customerCode == ""
	case "degraded":
		return customerCode == "provider_degraded" || customerCode == "provider_rate_limited"
	case "unavailable":
		return customerCode == "provider_unavailable"
	default:
		return false
	}
}

func (r *capabilityProviderHealthRepository) Clear(ctx context.Context, businessID, providerKey string) error {
	if err := validateCapabilityProviderHealthScope(businessID, providerKey); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Exec(`
		DELETE FROM capability_provider_health_snapshots
		WHERE business_id = ? AND provider_key = ?
	`, strings.TrimSpace(businessID), providerKey).Error
}

func validateCapabilityProviderHealthScope(businessID, providerKey string) error {
	if strings.TrimSpace(businessID) == "" || providerKey != gstProviderHealthKey {
		return interfaces.ErrCapabilityProviderHealthScope
	}
	return nil
}
