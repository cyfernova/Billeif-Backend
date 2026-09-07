package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	SubscriptionBillingModeFree          = "free"
	SubscriptionBillingModeLegacyOneTime = "legacy_one_time"
	SubscriptionBillingModeRenewable     = "renewable"

	SubscriptionStatusPendingPayment         = "pending_payment"
	SubscriptionStatusActive                 = "active"
	SubscriptionStatusRenewalPending         = "renewal_pending"
	SubscriptionStatusPastDue                = "past_due"
	SubscriptionStatusGracePeriod            = "grace_period"
	SubscriptionStatusCancellationScheduled  = "cancellation_scheduled"
	SubscriptionStatusCancelled              = "cancelled"
	SubscriptionStatusExpired                = "expired"
	SubscriptionStatusSuspended              = "suspended"
	SubscriptionStatusReconciliationRequired = "reconciliation_required"

	ProviderModeTest = "test"
	ProviderModeLive = "live"
)

type Subscription struct {
	ID                      string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID              string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Plan                    string         `gorm:"not null;size:50" json:"plan" validate:"required,oneof=free starter professional enterprise"`
	PlanCode                string         `gorm:"size:50;index" json:"plan_code,omitempty"`
	CatalogVersion          string         `gorm:"size:50" json:"catalog_version,omitempty"`
	Status                  string         `gorm:"not null;size:50;default:'active';index" json:"status"`
	BillingMode             string         `gorm:"not null;size:32;default:'free'" json:"billing_mode"`
	ProviderMode            string         `gorm:"size:16" json:"-"`
	ProviderCustomerID      string         `gorm:"size:160" json:"-"`
	ProviderSubscriptionID  string         `gorm:"size:160" json:"-"`
	ProviderPlanID          string         `gorm:"size:160" json:"-"`
	MaxInvoices             int64          `gorm:"default:10" json:"max_invoices" validate:"gte=0"`
	MaxCustomers            int64          `gorm:"default:10" json:"max_customers" validate:"gte=0"`
	MaxUsers                int64          `gorm:"default:3" json:"max_users" validate:"gte=0"`
	MaxStorageMB            int64          `gorm:"default:100" json:"max_storage_mb" validate:"gte=0"`
	StartDate               time.Time      `gorm:"not null" json:"start_date" validate:"required"`
	EndDate                 *time.Time     `json:"end_date,omitempty"`
	NextBillingDate         *time.Time     `json:"next_billing_date,omitempty"`
	PeriodStart             *time.Time     `json:"period_start,omitempty"`
	PeriodEnd               *time.Time     `json:"period_end,omitempty"`
	NextRenewalAt           *time.Time     `json:"next_renewal_at,omitempty"`
	GraceDeadline           *time.Time     `json:"grace_deadline,omitempty"`
	CancelAtPeriodEnd       bool           `json:"cancel_at_period_end"`
	CancellationEffectiveAt *time.Time     `json:"cancellation_effective_at,omitempty"`
	CancelledAt             *time.Time     `json:"cancelled_at,omitempty"`
	PendingPlanID           string         `gorm:"size:80" json:"pending_plan_id,omitempty"`
	PendingProviderPlanID   string         `gorm:"size:160" json:"-"`
	PendingPlanEffectiveAt  *time.Time     `json:"pending_plan_effective_at,omitempty"`
	LastProviderEventAt     *time.Time     `json:"-"`
	LastProviderPaidCount   int64          `json:"-"`
	ReconciliationCode      string         `gorm:"size:80" json:"reconciliation_code,omitempty"`
	LifecycleVersion        int64          `gorm:"not null;default:1" json:"lifecycle_version"`
	CreatedAt               time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt               time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt               gorm.DeletedAt `gorm:"index" json:"-"`
}

func (s *Subscription) TableName() string {
	return "subscriptions"
}

type SubscriptionQuotaUsage struct {
	BusinessID  string    `gorm:"primaryKey;type:uuid" json:"business_id"`
	FeatureKey  string    `gorm:"primaryKey;size:120" json:"feature_key"`
	PeriodStart time.Time `gorm:"primaryKey" json:"period_start"`
	UsedValue   int64     `gorm:"not null;default:0" json:"used_value"`
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (SubscriptionQuotaUsage) TableName() string {
	return "subscription_quota_usage"
}

type SubscriptionBillingRecord struct {
	ID                string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID        string     `gorm:"not null;type:uuid;index" json:"business_id"`
	SubscriptionID    string     `gorm:"not null;type:uuid;index" json:"subscription_id"`
	ProviderMode      string     `gorm:"not null;size:16" json:"-"`
	ProviderEventID   string     `gorm:"size:160" json:"-"`
	ProviderInvoiceID string     `gorm:"size:160" json:"-"`
	ProviderPaymentID string     `gorm:"size:160" json:"-"`
	AmountMinor       int64      `gorm:"not null" json:"amount_minor"`
	Currency          string     `gorm:"not null;size:3" json:"currency"`
	Status            string     `gorm:"not null;size:32" json:"status"`
	ReceiptReference  string     `gorm:"not null;size:80" json:"receipt_reference"`
	PeriodStart       *time.Time `json:"period_start,omitempty"`
	PeriodEnd         *time.Time `json:"period_end,omitempty"`
	QuotaPeriodStart  *time.Time `json:"quota_period_start,omitempty"`
	QuotaPeriodEnd    *time.Time `json:"quota_period_end,omitempty"`
	OccurredAt        time.Time  `gorm:"not null" json:"occurred_at"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

func (SubscriptionBillingRecord) TableName() string { return "subscription_billing_records" }

type SubscriptionAuditRecord struct {
	ID              string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID      string    `gorm:"not null;type:uuid;index" json:"business_id"`
	SubscriptionID  string    `gorm:"type:uuid;index" json:"subscription_id,omitempty"`
	ActorUserID     string    `gorm:"size:120" json:"actor_user_id,omitempty"`
	Action          string    `gorm:"not null;size:80" json:"action"`
	FromStatus      string    `gorm:"size:32" json:"from_status,omitempty"`
	ToStatus        string    `gorm:"size:32" json:"to_status,omitempty"`
	FromPlanID      string    `gorm:"size:80" json:"from_plan_id,omitempty"`
	ToPlanID        string    `gorm:"size:80" json:"to_plan_id,omitempty"`
	ProviderMode    string    `gorm:"size:16" json:"-"`
	ProviderEventID string    `gorm:"size:160" json:"-"`
	SanitizedCode   string    `gorm:"not null;size:80" json:"code"`
	OccurredAt      time.Time `gorm:"not null" json:"occurred_at"`
	CreatedAt       time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (SubscriptionAuditRecord) TableName() string { return "subscription_audit_records" }

type SubscriptionCommand struct {
	ID                       string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"-"`
	BusinessID               string     `gorm:"not null;type:uuid;index" json:"-"`
	SubscriptionID           string     `gorm:"type:uuid;index" json:"-"`
	ActorUserID              string     `gorm:"not null;size:120" json:"-"`
	Action                   string     `gorm:"not null;size:80" json:"-"`
	IdempotencyKey           string     `gorm:"not null;size:180" json:"-"`
	RequestHash              string     `gorm:"not null;size:64" json:"-"`
	Status                   string     `gorm:"not null;size:32" json:"-"`
	SanitizedErrorCode       string     `gorm:"size:80" json:"-"`
	ResponseStatus           string     `gorm:"size:32" json:"-"`
	ResponseBillingMode      string     `gorm:"size:32" json:"-"`
	ResponsePendingPlanID    string     `gorm:"size:80" json:"-"`
	ResponseAuthorizationURL string     `gorm:"type:text" json:"-"`
	CreatedAt                time.Time  `gorm:"autoCreateTime" json:"-"`
	UpdatedAt                time.Time  `gorm:"autoUpdateTime" json:"-"`
	CompletedAt              *time.Time `json:"-"`
}

func (SubscriptionCommand) TableName() string { return "subscription_commands" }
