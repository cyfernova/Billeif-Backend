package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"gorm.io/gorm"
)

const (
	FeatureEInvoice = "einvoice"
	FeatureEWayBill = "ewaybill"
	FeatureBulkGST  = "bulk_gst"
	FeatureGSTAPI   = "gst_api"
	FeaturePOS      = "pos"
)

type PlanEntitlements struct {
	EInvoiceEnabled bool  `json:"einvoice_enabled"`
	EInvoiceLimit   int64 `json:"einvoice_limit"`
	EWayBillEnabled bool  `json:"ewaybill_enabled"`
	EWayBillLimit   int64 `json:"ewaybill_limit"`
	BulkGSTEnabled  bool  `json:"bulk_gst_enabled"`
	GSTAPIEnabled   bool  `json:"gst_api_enabled"`
	POSEnabled      bool  `json:"pos_enabled"`
}

type EntitlementService struct {
	cfg              *config.Config
	db               *gorm.DB
	subscriptionRepo interfaces.SubscriptionRepository
	log              *logger.Logger
}

func NewEntitlementService(cfg *config.Config, db *gorm.DB, subscriptionRepo interfaces.SubscriptionRepository, log *logger.Logger) *EntitlementService {
	return &EntitlementService{
		cfg:              cfg,
		db:               db,
		subscriptionRepo: subscriptionRepo,
		log:              log,
	}
}

func (s *EntitlementService) ResolveByBusiness(ctx context.Context, businessID string) (PlanEntitlements, error) {
	if s.subscriptionRepo == nil {
		return defaultEntitlementsForPlan("free"), nil
	}
	subscription, err := s.subscriptionRepo.GetByBusinessID(ctx, businessID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") || strings.Contains(err.Error(), "subscription not found") {
			return defaultEntitlementsForPlan("free"), nil
		}
		return PlanEntitlements{}, err
	}
	return s.resolve(subscription.Plan), nil
}

func (s *EntitlementService) EnsureFeature(ctx context.Context, businessID, feature string) error {
	entitlements, err := s.ResolveByBusiness(ctx, businessID)
	if err != nil {
		return err
	}
	switch feature {
	case FeatureEInvoice:
		if !entitlements.EInvoiceEnabled {
			return fmt.Errorf("e-invoice generation is not enabled on the current plan")
		}
		return s.ensureMonthlyLimit(ctx, businessID, entitlements.EInvoiceLimit, models.EInvoiceStatusGenerated, &models.EInvoiceRecord{}, "e-invoice monthly quota exceeded")
	case FeatureEWayBill:
		if !entitlements.EWayBillEnabled {
			return fmt.Errorf("e-way bill generation is not enabled on the current plan")
		}
		return s.ensureMonthlyLimit(ctx, businessID, entitlements.EWayBillLimit, models.EWayBillStatusGenerated, &models.EWayBillRecord{}, "e-way bill monthly quota exceeded")
	case FeatureBulkGST:
		if !entitlements.BulkGSTEnabled {
			return fmt.Errorf("bulk GST actions are not enabled on the current plan")
		}
	case FeatureGSTAPI:
		if !entitlements.GSTAPIEnabled {
			return fmt.Errorf("GST API access is not enabled on the current plan")
		}
	case FeaturePOS:
		if !entitlements.POSEnabled {
			return fmt.Errorf("POS is not enabled on the current plan")
		}
	}
	return nil
}

func (s *EntitlementService) resolve(plan string) PlanEntitlements {
	defaults := defaultEntitlementsForPlan(plan)
	if s.cfg == nil {
		return defaults
	}
	raw := strings.TrimSpace(s.cfg.Entitlements.JSON)
	if raw == "" {
		return defaults
	}
	var overrides map[string]PlanEntitlements
	if err := json.Unmarshal([]byte(raw), &overrides); err != nil {
		s.log.Warn("failed to parse entitlements json; using defaults", "error", err)
		return defaults
	}
	override, ok := overrides[strings.ToLower(strings.TrimSpace(plan))]
	if !ok {
		return defaults
	}
	return override
}

func (s *EntitlementService) ensureMonthlyLimit(ctx context.Context, businessID string, limit int64, successStatus string, model interface{}, message string) error {
	if limit <= 0 {
		return nil
	}
	start := time.Now().UTC()
	monthStart := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	var count int64
	if err := s.db.WithContext(ctx).
		Model(model).
		Where("business_id = ? AND status = ? AND created_at >= ? AND deleted_at IS NULL", businessID, successStatus, monthStart).
		Count(&count).Error; err != nil {
		return err
	}
	if count >= limit {
		return fmt.Errorf("%s", message)
	}
	return nil
}

func defaultEntitlementsForPlan(plan string) PlanEntitlements {
	return PlanEntitlements{
		EInvoiceEnabled: true,
		EInvoiceLimit:   -1,
		EWayBillEnabled: true,
		EWayBillLimit:   -1,
		BulkGSTEnabled:  true,
		GSTAPIEnabled:   true,
		POSEnabled:      true,
	}
}
