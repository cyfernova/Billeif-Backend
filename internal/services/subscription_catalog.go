package services

import (
	"strings"
	"time"

	"invoice-backend/internal/models"
)

const CurrentSubscriptionCatalogVersion = "swipe-v1"

const (
	QuotaInvoices        = "invoices"
	QuotaCustomers       = "customers"
	QuotaUsers           = "users"
	QuotaStorageMB       = "storage_mb"
	QuotaEInvoiceMonthly = "einvoice_monthly"
	QuotaEWayBillMonthly = "ewaybill_monthly"
)

type SubscriptionPlan struct {
	ID          string           `json:"id"`
	DisplayName string           `json:"display_name"`
	LegacyPlan  string           `json:"legacy_plan"`
	PlanCode    string           `json:"plan_code"`
	Currency    string           `json:"currency"`
	Amount      int64            `json:"amount"`
	Interval    string           `json:"interval"`
	Features    map[string]bool  `json:"features"`
	Quotas      map[string]int64 `json:"quotas"`
}

type SubscriptionCatalogResponse struct {
	Version string             `json:"version"`
	Plans   []SubscriptionPlan `json:"plans"`
}

var subscriptionPlans = []SubscriptionPlan{
	newSubscriptionPlan("free", "Free", "free", "free", 0, 10, 10, 3, 100, false),
	newSubscriptionPlan("pro_monthly", "Pro", "starter", "pro", 29900, 100, 100, 3, 512, true),
	newSubscriptionPlan("rise_monthly", "Rise", "professional", "rise", 99900, 1000, 1000, 10, 2048, true),
	newSubscriptionPlan("biz_monthly", "Biz", "enterprise", "biz", 299900, 100000, 100000, 50, 10240, true),
}

func newSubscriptionPlan(id, displayName, legacyPlan, planCode string, amount, invoices, customers, users, storageMB int64, paidFeatures bool) SubscriptionPlan {
	features := map[string]bool{
		FeatureEInvoice:              paidFeatures,
		FeatureEWayBill:              paidFeatures,
		FeatureBulkGST:               paidFeatures,
		FeatureGSTAPI:                paidFeatures,
		FeaturePOS:                   paidFeatures,
		FeatureOnlineStore:           paidFeatures,
		FeatureMultiCurrency:         paidFeatures,
		FeatureExportDocuments:       paidFeatures,
		FeatureSEZDocuments:          paidFeatures,
		FeatureDeemedExportDocuments: paidFeatures,
		FeatureMultiUser:             paidFeatures,
		FeatureCustomRoles:           paidFeatures,
		FeatureMultiBusiness:         paidFeatures,
		FeatureBranches:              paidFeatures,
		FeaturePrioritySupport:       paidFeatures,
		FeatureDriveStorageMB:        paidFeatures,
		FeatureWhatsAppNotifications: paidFeatures,
	}
	return SubscriptionPlan{
		ID:          id,
		DisplayName: displayName,
		LegacyPlan:  legacyPlan,
		PlanCode:    planCode,
		Currency:    "INR",
		Amount:      amount,
		Interval:    "month",
		Features:    features,
		Quotas: map[string]int64{
			QuotaInvoices:        invoices,
			QuotaCustomers:       customers,
			QuotaUsers:           users,
			QuotaStorageMB:       storageMB,
			QuotaEInvoiceMonthly: invoices,
			QuotaEWayBillMonthly: invoices,
		},
	}
}

func SubscriptionCatalog() SubscriptionCatalogResponse {
	plans := make([]SubscriptionPlan, 0, len(subscriptionPlans))
	for _, plan := range subscriptionPlans {
		plans = append(plans, cloneSubscriptionPlan(plan))
	}
	return SubscriptionCatalogResponse{Version: CurrentSubscriptionCatalogVersion, Plans: plans}
}

func FindSubscriptionPlan(id string) (SubscriptionPlan, bool) {
	normalized := strings.ToLower(strings.TrimSpace(id))
	for _, plan := range subscriptionPlans {
		if plan.ID == normalized {
			return cloneSubscriptionPlan(plan), true
		}
	}
	return SubscriptionPlan{}, false
}

func paidSubscriptionPlan(id string) (SubscriptionPlan, bool) {
	plan, ok := FindSubscriptionPlan(id)
	return plan, ok && plan.Amount > 0
}

func subscriptionPlanForCode(code string) SubscriptionPlan {
	normalized := strings.ToLower(strings.TrimSpace(code))
	for _, plan := range subscriptionPlans {
		if plan.PlanCode == normalized || plan.LegacyPlan == normalized {
			return cloneSubscriptionPlan(plan)
		}
	}
	plan, _ := FindSubscriptionPlan("free")
	return plan
}

func subscriptionPlanForSubscription(subscription *models.Subscription, now time.Time) SubscriptionPlan {
	if subscription == nil {
		return subscriptionPlanForCode("free")
	}
	now = now.UTC()
	periodEnd := subscription.PeriodEnd
	if periodEnd == nil {
		periodEnd = subscription.EndDate
	}
	status := strings.ToLower(strings.TrimSpace(subscription.Status))
	hasAccess := false
	switch status {
	case models.SubscriptionStatusActive, models.SubscriptionStatusRenewalPending, models.SubscriptionStatusCancellationScheduled:
		hasAccess = periodEnd == nil || periodEnd.After(now)
	case models.SubscriptionStatusPastDue, models.SubscriptionStatusGracePeriod:
		hasAccess = subscription.GraceDeadline != nil && subscription.GraceDeadline.After(now)
	case models.SubscriptionStatusReconciliationRequired:
		hasAccess = subscription.LastProviderPaidCount > 0 && periodEnd != nil && periodEnd.After(now)
	}
	if !hasAccess {
		return subscriptionPlanForCode("free")
	}
	return subscriptionPlanForCode(normalizePlanCode(subscription.Plan, subscription.PlanCode))
}

func cloneSubscriptionPlan(plan SubscriptionPlan) SubscriptionPlan {
	clone := plan
	clone.Features = make(map[string]bool, len(plan.Features))
	for key, value := range plan.Features {
		clone.Features[key] = value
	}
	clone.Quotas = make(map[string]int64, len(plan.Quotas))
	for key, value := range plan.Quotas {
		clone.Quotas[key] = value
	}
	return clone
}

func normalizePlanCode(plan, planCode string) string {
	if code := strings.ToLower(strings.TrimSpace(planCode)); code != "" {
		return code
	}
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "starter":
		return "pro"
	case "professional":
		return "rise"
	case "enterprise":
		return "biz"
	case "free":
		return "free"
	default:
		return "free"
	}
}

func normalizeCatalogVersion(value string) string {
	if strings.TrimSpace(value) == "" {
		return CurrentSubscriptionCatalogVersion
	}
	return value
}
