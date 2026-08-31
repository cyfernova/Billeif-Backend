package models

import (
	"time"

	"gorm.io/gorm"
)

type Subscription struct {
	ID              string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID      string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Plan            string         `gorm:"not null;size:50" json:"plan" validate:"required,oneof=free starter professional enterprise"`
	PlanCode        string         `gorm:"size:50;index" json:"plan_code,omitempty"`
	CatalogVersion  string         `gorm:"size:50" json:"catalog_version,omitempty"`
	Status          string         `gorm:"not null;size:50;default:'active';index" json:"status" validate:"required,oneof=active canceled expired"`
	MaxInvoices     int64          `gorm:"default:10" json:"max_invoices" validate:"gte=0"`
	MaxCustomers    int64          `gorm:"default:10" json:"max_customers" validate:"gte=0"`
	MaxUsers        int64          `gorm:"default:3" json:"max_users" validate:"gte=0"`
	MaxStorageMB    int64          `gorm:"default:100" json:"max_storage_mb" validate:"gte=0"`
	StartDate       time.Time      `gorm:"not null" json:"start_date" validate:"required"`
	EndDate         *time.Time     `json:"end_date,omitempty"`
	NextBillingDate *time.Time     `json:"next_billing_date,omitempty"`
	CreatedAt       time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (s *Subscription) TableName() string {
	return "subscriptions"
}

type SubscriptionQuotaUsage struct {
	BusinessID  string    `gorm:"primaryKey;type:uuid" json:"business_id"`
	FeatureKey  string    `gorm:"primaryKey;size:120" json:"feature_key"`
	PeriodStart time.Time `gorm:"primaryKey;type:date" json:"period_start"`
	UsedValue   int64     `gorm:"not null;default:0" json:"used_value"`
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (SubscriptionQuotaUsage) TableName() string {
	return "subscription_quota_usage"
}
