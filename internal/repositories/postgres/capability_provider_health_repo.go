package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
		SELECT h.business_id, h.provider_key, h.integration_account_id,
			h.credential_revision, h.observation_revision, h.status,
			h.observed_at, h.fresh_until, h.retry_at, h.customer_code,
			h.created_at, h.updated_at
		FROM capability_provider_health_snapshots AS h
		JOIN gst_integration_accounts AS a
			ON a.id = h.integration_account_id
			AND a.business_id = h.business_id
			AND a.credential_revision = h.credential_revision
			AND a.deleted_at IS NULL
		WHERE h.business_id = ? AND h.provider_key = ?
	`, strings.TrimSpace(businessID), providerKey).Scan(&snapshot)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, interfaces.ErrCapabilityProviderHealthNotFound
	}
	return &snapshot, nil
}

func (r *capabilityProviderHealthRepository) RecordRevisionBound(
	ctx context.Context,
	snapshot *models.CapabilityProviderHealthSnapshot,
) (int64, error) {
	if err := validateCapabilityProviderHealthSnapshot(snapshot); err != nil {
		return 0, err
	}
	var revision int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		revision, err = r.recordRevisionBoundTx(ctx, tx, snapshot)
		return err
	})
	return revision, err
}

func (r *capabilityProviderHealthRepository) RecordValidationRevisionBound(
	ctx context.Context,
	snapshot *models.CapabilityProviderHealthSnapshot,
	state interfaces.GSTIntegrationValidationState,
) (int64, error) {
	if err := validateCapabilityProviderHealthSnapshot(snapshot); err != nil {
		return 0, err
	}
	if state.Status != models.GSTJobStatusSucceeded && state.Status != models.GSTJobStatusFailed {
		return 0, errors.New("GST integration validation status is invalid")
	}
	var revision int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		revision, err = r.recordRevisionBoundTx(ctx, tx, snapshot)
		if err != nil {
			return err
		}
		result := tx.WithContext(ctx).Exec(`
			UPDATE gst_integration_accounts
			SET status = ?, last_validated_at = ?, last_error = ?, updated_at = ?
			WHERE id = ? AND business_id = ? AND credential_revision = ? AND deleted_at IS NULL
		`, state.Status, state.LastValidatedAt, state.LastError, snapshot.UpdatedAt,
			snapshot.IntegrationAccountID, strings.TrimSpace(snapshot.BusinessID), snapshot.CredentialRevision)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return interfaces.ErrCapabilityProviderHealthObservationNotApplied
		}
		return nil
	})
	return revision, err
}

func validateCapabilityProviderHealthSnapshot(snapshot *models.CapabilityProviderHealthSnapshot) error {
	if snapshot == nil {
		return errors.New("capability provider health snapshot is required")
	}
	if err := validateCapabilityProviderHealthScope(snapshot.BusinessID, snapshot.ProviderKey); err != nil {
		return err
	}
	if strings.TrimSpace(snapshot.IntegrationAccountID) == "" || snapshot.CredentialRevision <= 0 {
		return errors.New("capability provider health credential revision is invalid")
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
	return nil
}

func (r *capabilityProviderHealthRepository) recordRevisionBoundTx(
	ctx context.Context,
	tx *gorm.DB,
	snapshot *models.CapabilityProviderHealthSnapshot,
) (int64, error) {
	var account struct {
		CredentialRevision int64 `gorm:"column:credential_revision"`
	}
	result := tx.WithContext(ctx).Raw(`
		SELECT credential_revision
		FROM gst_integration_accounts
		WHERE id = ? AND business_id = ? AND deleted_at IS NULL
		FOR UPDATE
	`, strings.TrimSpace(snapshot.IntegrationAccountID), strings.TrimSpace(snapshot.BusinessID)).Scan(&account)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 {
		return 0, interfaces.ErrGSTIntegrationAccountNotFound
	}
	if account.CredentialRevision != snapshot.CredentialRevision {
		return 0, interfaces.ErrCapabilityProviderHealthCredentialRevisionStale
	}
	var persisted struct {
		ObservationRevision int64 `gorm:"column:observation_revision"`
	}
	result = tx.WithContext(ctx).Raw(`
		INSERT INTO capability_provider_health_snapshots (
			business_id, provider_key, integration_account_id, credential_revision,
			observation_revision, status, observed_at, fresh_until, retry_at,
			customer_code, created_at, updated_at
		) VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (business_id, provider_key) DO UPDATE SET
			integration_account_id = EXCLUDED.integration_account_id,
			credential_revision = EXCLUDED.credential_revision,
			observation_revision = capability_provider_health_snapshots.observation_revision + 1,
			status = EXCLUDED.status,
			observed_at = EXCLUDED.observed_at,
			fresh_until = EXCLUDED.fresh_until,
			retry_at = EXCLUDED.retry_at,
			customer_code = EXCLUDED.customer_code,
			updated_at = EXCLUDED.updated_at
		RETURNING observation_revision
	`, strings.TrimSpace(snapshot.BusinessID), snapshot.ProviderKey,
		strings.TrimSpace(snapshot.IntegrationAccountID), snapshot.CredentialRevision, snapshot.Status,
		snapshot.ObservedAt, snapshot.FreshUntil, snapshot.RetryAt, snapshot.CustomerCode,
		snapshot.CreatedAt, snapshot.UpdatedAt).Scan(&persisted)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected != 1 || persisted.ObservationRevision <= 0 {
		return 0, interfaces.ErrCapabilityProviderHealthObservationNotApplied
	}
	snapshot.ObservationRevision = persisted.ObservationRevision
	return persisted.ObservationRevision, nil
}

func (r *capabilityProviderHealthRepository) SaveGSTIntegrationAccountAndInvalidate(
	ctx context.Context,
	account *models.GSTIntegrationAccount,
	expectedCredentialRevision int64,
) error {
	if account == nil || strings.TrimSpace(account.ID) == "" || strings.TrimSpace(account.BusinessID) == "" || expectedCredentialRevision < 0 {
		return errors.New("GST integration account revision scope is invalid")
	}
	now := account.UpdatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
		account.UpdatedAt = now
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var business struct {
			ID string `gorm:"column:id"`
		}
		result := tx.WithContext(ctx).Raw(`
			SELECT id FROM business_profiles
			WHERE id = ? AND deleted_at IS NULL
			FOR UPDATE
		`, strings.TrimSpace(account.BusinessID)).Scan(&business)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return interfaces.ErrCapabilityProviderHealthScope
		}
		var locked []struct {
			ID                 string `gorm:"column:id"`
			CredentialRevision int64  `gorm:"column:credential_revision"`
		}
		result = tx.WithContext(ctx).Raw(`
			SELECT id, credential_revision
			FROM gst_integration_accounts
			WHERE business_id = ? AND deleted_at IS NULL
			FOR UPDATE
		`, strings.TrimSpace(account.BusinessID)).Scan(&locked)
		if result.Error != nil {
			return result.Error
		}
		if expectedCredentialRevision > 0 {
			found := false
			for _, current := range locked {
				if current.ID != account.ID {
					continue
				}
				found = true
				if current.CredentialRevision != expectedCredentialRevision {
					return interfaces.ErrCapabilityProviderHealthCredentialRevisionStale
				}
				break
			}
			if !found {
				return interfaces.ErrGSTIntegrationAccountNotFound
			}
		}
		if len(locked) > 0 {
			result = tx.WithContext(ctx).Exec(`
				UPDATE gst_integration_accounts
				SET credential_revision = credential_revision + 1
				WHERE business_id = ? AND deleted_at IS NULL
			`, strings.TrimSpace(account.BusinessID))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != int64(len(locked)) {
				return interfaces.ErrCapabilityProviderHealthObservationNotApplied
			}
		}
		if expectedCredentialRevision > 0 {
			account.CredentialRevision = expectedCredentialRevision + 1
			result = tx.WithContext(ctx).Exec(`
				UPDATE gst_integration_accounts
				SET provider = ?, service_type = ?, gsp_name = ?, portal_username = ?,
					encrypted_credentials = ?, credential_hint = ?, status = ?,
					last_validated_at = NULL, last_error = '', metadata = ?, updated_at = ?
				WHERE id = ? AND business_id = ? AND credential_revision = ? AND deleted_at IS NULL
			`, account.Provider, account.ServiceType, account.GSPName, account.PortalUsername,
				account.EncryptedCredentials, account.CredentialHint, account.Status, account.Metadata, now,
				account.ID, strings.TrimSpace(account.BusinessID), account.CredentialRevision)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return interfaces.ErrCapabilityProviderHealthObservationNotApplied
			}
		} else {
			account.CredentialRevision = 1
			if account.CreatedAt.IsZero() {
				account.CreatedAt = now
			}
			if err := tx.WithContext(ctx).Create(account).Error; err != nil {
				return err
			}
		}
		result = tx.WithContext(ctx).Exec(`
			DELETE FROM capability_provider_health_snapshots
			WHERE business_id = ? AND provider_key = ?
		`, strings.TrimSpace(account.BusinessID), gstProviderHealthKey)
		if result.Error != nil {
			return fmt.Errorf("invalidate GST provider health: %w", result.Error)
		}
		return nil
	})
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

func validateCapabilityProviderHealthScope(businessID, providerKey string) error {
	if strings.TrimSpace(businessID) == "" || providerKey != gstProviderHealthKey {
		return interfaces.ErrCapabilityProviderHealthScope
	}
	return nil
}
