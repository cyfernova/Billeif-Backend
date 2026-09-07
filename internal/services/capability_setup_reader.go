package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type DBCapabilityBusinessSetupReader struct {
	db *gorm.DB
}

func NewDBCapabilityBusinessSetupReader(db *gorm.DB) *DBCapabilityBusinessSetupReader {
	return &DBCapabilityBusinessSetupReader{db: db}
}

func (r *DBCapabilityBusinessSetupReader) ReadCapabilityBusinessSetup(ctx context.Context, businessID string) (CapabilityBusinessSetup, error) {
	if r == nil || r.db == nil {
		return CapabilityBusinessSetup{}, fmt.Errorf("capability setup database is required")
	}
	var business struct {
		GSTRegistered bool
		GSTIN         string
	}
	err := r.db.WithContext(ctx).Table("business_profiles").
		Select("gst_registered, gstin").
		Where("id = ? AND deleted_at IS NULL", businessID).
		Take(&business).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return CapabilityBusinessSetup{}, nil
	}
	if err != nil {
		return CapabilityBusinessSetup{}, fmt.Errorf("read business capability setup: %w", err)
	}

	gstAccount, err := r.exists(ctx, "gst_integration_accounts",
		"business_id = ? AND deleted_at IS NULL AND provider <> ? AND encrypted_credentials <> ''", businessID, "simulated")
	if err != nil {
		return CapabilityBusinessSetup{}, err
	}
	whatsApp, err := r.exists(ctx, "whatsapp_configs",
		"business_id = ? AND deleted_at IS NULL AND enabled = ? AND phone_number_id <> '' AND access_token <> ''", businessID, true)
	if err != nil {
		return CapabilityBusinessSetup{}, err
	}
	email, err := r.exists(ctx, "email_accounts",
		"business_id = ? AND deleted_at IS NULL AND status = ?", businessID, "connected")
	if err != nil {
		return CapabilityBusinessSetup{}, err
	}
	storefrontPayments, err := r.exists(ctx, "storefronts",
		"business_id = ? AND deleted_at IS NULL AND allow_online_payment = ?", businessID, true)
	if err != nil {
		return CapabilityBusinessSetup{}, err
	}

	return CapabilityBusinessSetup{
		BusinessExists:     true,
		GST:                business.GSTRegistered && strings.TrimSpace(business.GSTIN) != "" && gstAccount,
		WhatsApp:           whatsApp,
		Email:              email,
		Voice:              true,
		StorefrontPayments: storefrontPayments,
	}, nil
}

func (r *DBCapabilityBusinessSetupReader) exists(ctx context.Context, table, where string, args ...interface{}) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Table(table).Where(where, args...).Limit(1).Count(&count).Error; err != nil {
		return false, fmt.Errorf("read %s capability setup: %w", table, err)
	}
	return count > 0, nil
}
