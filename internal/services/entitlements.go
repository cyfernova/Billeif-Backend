package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	FeatureEInvoice = "einvoice"
	FeatureEWayBill = "ewaybill"
	FeatureBulkGST  = "bulk_gst"
	FeatureGSTAPI   = "gst_api"
	FeaturePOS      = "pos"
)

type PlanEntitlements struct {
	PlanID          string `json:"plan_id"`
	EInvoiceEnabled bool   `json:"einvoice_enabled"`
	EInvoiceLimit   int64  `json:"einvoice_limit"`
	EWayBillEnabled bool   `json:"ewaybill_enabled"`
	EWayBillLimit   int64  `json:"ewaybill_limit"`
	BulkGSTEnabled  bool   `json:"bulk_gst_enabled"`
	GSTAPIEnabled   bool   `json:"gst_api_enabled"`
	POSEnabled      bool   `json:"pos_enabled"`
}

type CapabilityQuota struct {
	Unit      string `json:"unit,omitempty"`
	Limited   bool   `json:"limited"`
	Limit     int64  `json:"limit"`
	Used      int64  `json:"used"`
	Remaining int64  `json:"remaining"`
	Available bool   `json:"available"`
}

type DriveStorageQuota struct {
	Access     FeatureAccess
	LimitBytes int64
	UsedBytes  int64
}

type DriveStorageQuotaReader interface {
	InspectDriveStorage(ctx context.Context, businessID string) (DriveStorageQuota, error)
}

type FeatureAccess struct {
	Required bool            `json:"required"`
	Entitled bool            `json:"entitled"`
	Quota    CapabilityQuota `json:"quota"`
}

type FeatureUnavailableError struct {
	Code    string `json:"code"`
	Feature string `json:"feature"`
	PlanID  string `json:"plan_id"`
}

func (e *FeatureUnavailableError) Error() string {
	return fmt.Sprintf("%s is not enabled on plan %s", e.Feature, e.PlanID)
}

type QuotaExceededError struct {
	Code    string `json:"code"`
	Feature string `json:"feature"`
	Limit   int64  `json:"limit"`
	Used    int64  `json:"used"`
	PlanID  string `json:"plan_id"`
}

func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("%s quota exceeded for plan %s", e.Feature, e.PlanID)
}

type EntitlementService struct {
	db               *gorm.DB
	subscriptionRepo interfaces.SubscriptionRepository
	log              *logger.Logger
}

func NewEntitlementService(_ *config.Config, db *gorm.DB, subscriptionRepo interfaces.SubscriptionRepository, log *logger.Logger) *EntitlementService {
	return &EntitlementService{
		db:               db,
		subscriptionRepo: subscriptionRepo,
		log:              log,
	}
}

func (s *EntitlementService) ResolveByBusiness(ctx context.Context, businessID string) (PlanEntitlements, error) {
	if s.subscriptionRepo == nil {
		return entitlementsForPlan(subscriptionPlanForCode("free")), nil
	}
	subscription, err := s.subscriptionRepo.GetByBusinessID(ctx, businessID)
	if err != nil {
		if isSubscriptionNotFoundError(err) {
			return entitlementsForPlan(subscriptionPlanForCode("free")), nil
		}
		return PlanEntitlements{}, err
	}
	return entitlementsForPlan(subscriptionPlanForSubscription(subscription, time.Now().UTC())), nil
}

// InspectFeature observes current catalog entitlement and quota usage without
// reserving capacity. Mutations must still reserve through ReserveFeatureTx.
func (s *EntitlementService) InspectFeature(ctx context.Context, businessID, feature string) (FeatureAccess, error) {
	if feature == FeatureDriveStorageMB {
		storage, err := s.InspectDriveStorage(ctx, businessID)
		return storage.Access, err
	}
	plan, periodStart, err := s.resolvePlanAndQuotaStart(ctx, businessID, time.Now().UTC())
	if err != nil {
		return FeatureAccess{}, err
	}
	access := FeatureAccess{Required: true, Entitled: plan.Features[feature]}
	limit, limited := quotaLimitForFeature(plan, feature)
	access.Quota = CapabilityQuota{Limited: limited, Limit: limit, Available: access.Entitled}
	if !limited || !access.Entitled {
		return access, nil
	}
	if s.db == nil {
		return FeatureAccess{}, fmt.Errorf("quota database is required")
	}
	var usage models.SubscriptionQuotaUsage
	err = s.db.WithContext(ctx).
		Where("business_id = ? AND feature_key = ? AND period_start = ?", businessID, feature, periodStart).
		First(&usage).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return FeatureAccess{}, err
	}
	access.Quota.Used = usage.UsedValue
	access.Quota.Remaining = max(limit-usage.UsedValue, 0)
	access.Quota.Available = usage.UsedValue < limit
	return access, nil
}

func (s *EntitlementService) InspectDriveStorage(ctx context.Context, businessID string) (DriveStorageQuota, error) {
	plan, err := s.resolvePlan(ctx, businessID)
	if err != nil {
		return DriveStorageQuota{}, err
	}
	limitMB, limited := plan.Quotas[QuotaStorageMB]
	access := FeatureAccess{
		Required: true,
		Entitled: plan.Features[FeatureDriveStorageMB],
		Quota: CapabilityQuota{
			Unit: "MB", Limited: limited, Limit: limitMB,
		},
	}
	if s.db == nil {
		return DriveStorageQuota{}, fmt.Errorf("storage quota database is required")
	}
	var usedBytes int64
	if err := s.db.WithContext(ctx).
		Model(&models.DriveAsset{}).
		Select("COALESCE(SUM(size_bytes), 0)").
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Scan(&usedBytes).Error; err != nil {
		return DriveStorageQuota{}, fmt.Errorf("read drive storage usage: %w", err)
	}
	const bytesPerMB int64 = 1024 * 1024
	limitBytes := limitMB * bytesPerMB
	usedMB := usedBytes / bytesPerMB
	if usedBytes%bytesPerMB != 0 {
		usedMB++
	}
	remainingBytes := max(limitBytes-usedBytes, 0)
	access.Quota.Used = usedMB
	access.Quota.Remaining = remainingBytes / bytesPerMB
	access.Quota.Available = access.Entitled && (!limited || usedBytes < limitBytes)
	return DriveStorageQuota{Access: access, LimitBytes: limitBytes, UsedBytes: usedBytes}, nil
}

func (s *EntitlementService) resolvePlan(ctx context.Context, businessID string) (SubscriptionPlan, error) {
	plan, _, err := s.resolvePlanAndQuotaStart(ctx, businessID, time.Now().UTC())
	return plan, err
}

func (s *EntitlementService) resolvePlanAndQuotaStart(ctx context.Context, businessID string, now time.Time) (SubscriptionPlan, time.Time, error) {
	if s.subscriptionRepo == nil {
		return subscriptionPlanForCode("free"), currentQuotaPeriodStart(now), nil
	}
	subscription, err := s.subscriptionRepo.GetByBusinessID(ctx, businessID)
	if err != nil {
		if isSubscriptionNotFoundError(err) {
			return subscriptionPlanForCode("free"), currentQuotaPeriodStart(now), nil
		}
		return SubscriptionPlan{}, time.Time{}, err
	}
	return subscriptionPlanForSubscription(subscription, now), quotaPeriodStart(subscription, now), nil
}

func (s *EntitlementService) EnsureFeature(ctx context.Context, businessID, feature string) error {
	entitlements, err := s.ResolveByBusiness(ctx, businessID)
	if err != nil {
		return err
	}
	if entitlementFeatureEnabled(entitlements, feature) {
		return nil
	}
	return &FeatureUnavailableError{Code: "feature_disabled", Feature: feature, PlanID: entitlements.PlanID}
}

func (s *EntitlementService) ReserveFeatureTx(ctx context.Context, tx *gorm.DB, businessID, feature string, amount int64) error {
	if tx == nil {
		return fmt.Errorf("quota transaction is required")
	}
	if amount <= 0 {
		return fmt.Errorf("quota reservation amount must be positive")
	}

	plan, periodStart, err := s.resolvePlanTx(ctx, tx, businessID)
	if err != nil {
		return err
	}
	if !plan.Features[feature] {
		return &FeatureUnavailableError{Code: "feature_disabled", Feature: feature, PlanID: plan.ID}
	}

	limit, limited := quotaLimitForFeature(plan, feature)
	if !limited {
		return nil
	}
	if limit < amount {
		return &QuotaExceededError{Code: "quota_exceeded", Feature: feature, Limit: limit, Used: 0, PlanID: plan.ID}
	}

	usage := models.SubscriptionQuotaUsage{
		BusinessID: businessID, FeatureKey: feature, PeriodStart: periodStart, UsedValue: amount,
	}
	result := tx.WithContext(ctx).Clauses(
		clause.OnConflict{
			Columns: []clause.Column{{Name: "business_id"}, {Name: "feature_key"}, {Name: "period_start"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"used_value": gorm.Expr("subscription_quota_usage.used_value + ?", amount),
				"updated_at": time.Now().UTC(),
			}),
			Where: clause.Where{Exprs: []clause.Expression{
				gorm.Expr("subscription_quota_usage.used_value + ? <= ?", amount, limit),
			}},
		},
		clause.Returning{Columns: []clause.Column{{Name: "used_value"}}},
	).Create(&usage)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}

	var current models.SubscriptionQuotaUsage
	if err := tx.WithContext(ctx).
		Where("business_id = ? AND feature_key = ? AND period_start = ?", businessID, feature, periodStart).
		First(&current).Error; err != nil {
		return err
	}
	return &QuotaExceededError{Code: "quota_exceeded", Feature: feature, Limit: limit, Used: current.UsedValue, PlanID: plan.ID}
}

func (s *EntitlementService) resolvePlanTx(ctx context.Context, tx *gorm.DB, businessID string) (SubscriptionPlan, time.Time, error) {
	var subscription models.Subscription
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		First(&subscription).Error
	switch {
	case err == nil:
		now := time.Now().UTC()
		return subscriptionPlanForSubscription(&subscription, now), quotaPeriodStart(&subscription, now), nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return subscriptionPlanForCode("free"), currentQuotaPeriodStart(time.Now().UTC()), nil
	default:
		return SubscriptionPlan{}, time.Time{}, err
	}
}

func quotaPeriodStart(subscription *models.Subscription, now time.Time) time.Time {
	if subscription != nil && subscription.BillingMode != models.SubscriptionBillingModeFree && subscription.PeriodStart != nil {
		return subscription.PeriodStart.UTC()
	}
	return currentQuotaPeriodStart(now)
}

func entitlementsForPlan(plan SubscriptionPlan) PlanEntitlements {
	return PlanEntitlements{
		PlanID:          plan.ID,
		EInvoiceEnabled: plan.Features[FeatureEInvoice],
		EInvoiceLimit:   plan.Quotas[QuotaEInvoiceMonthly],
		EWayBillEnabled: plan.Features[FeatureEWayBill],
		EWayBillLimit:   plan.Quotas[QuotaEWayBillMonthly],
		BulkGSTEnabled:  plan.Features[FeatureBulkGST],
		GSTAPIEnabled:   plan.Features[FeatureGSTAPI],
		POSEnabled:      plan.Features[FeaturePOS],
	}
}

func entitlementFeatureEnabled(entitlements PlanEntitlements, feature string) bool {
	switch feature {
	case FeatureEInvoice:
		return entitlements.EInvoiceEnabled
	case FeatureEWayBill:
		return entitlements.EWayBillEnabled
	case FeatureBulkGST:
		return entitlements.BulkGSTEnabled
	case FeatureGSTAPI:
		return entitlements.GSTAPIEnabled
	case FeaturePOS:
		return entitlements.POSEnabled
	default:
		return false
	}
}

func quotaLimitForFeature(plan SubscriptionPlan, feature string) (int64, bool) {
	var key string
	switch feature {
	case FeatureEInvoice:
		key = QuotaEInvoiceMonthly
	case FeatureEWayBill:
		key = QuotaEWayBillMonthly
	default:
		return 0, false
	}
	limit, ok := plan.Quotas[key]
	return limit, ok
}

func currentQuotaPeriodStart(now time.Time) time.Time {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func isSubscriptionNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	normalized := strings.ToLower(err.Error())
	return strings.Contains(normalized, "subscription not found") || strings.Contains(normalized, "record not found")
}
