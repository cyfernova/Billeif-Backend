package models

import "time"

// CapabilityProviderHealthSnapshot is sanitized derived state. It must never
// contain provider credentials, account identifiers, response bodies, or raw
// errors.
type CapabilityProviderHealthSnapshot struct {
	BusinessID   string     `gorm:"column:business_id;type:uuid;primaryKey" json:"-"`
	ProviderKey  string     `gorm:"column:provider_key;size:64;primaryKey" json:"-"`
	Status       string     `gorm:"column:status;size:32;not null" json:"-"`
	ObservedAt   time.Time  `gorm:"column:observed_at;not null" json:"-"`
	FreshUntil   time.Time  `gorm:"column:fresh_until;not null" json:"-"`
	RetryAt      *time.Time `gorm:"column:retry_at" json:"-"`
	CustomerCode string     `gorm:"column:customer_code;size:64;not null" json:"-"`
	CreatedAt    time.Time  `gorm:"column:created_at;not null" json:"-"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;not null" json:"-"`
}

func (CapabilityProviderHealthSnapshot) TableName() string {
	return "capability_provider_health_snapshots"
}
