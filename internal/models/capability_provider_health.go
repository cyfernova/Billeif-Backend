package models

import "time"

// CapabilityProviderHealthSnapshot is sanitized derived state. The account ID
// and revisions are internal consistency keys and must never be exposed. The
// snapshot must never contain provider credentials, response bodies, or raw
// errors.
type CapabilityProviderHealthSnapshot struct {
	BusinessID           string     `gorm:"column:business_id;type:uuid;primaryKey" json:"-"`
	ProviderKey          string     `gorm:"column:provider_key;size:64;primaryKey" json:"-"`
	IntegrationAccountID string     `gorm:"column:integration_account_id;type:uuid;not null" json:"-"`
	CredentialRevision   int64      `gorm:"column:credential_revision;not null" json:"-"`
	ObservationRevision  int64      `gorm:"column:observation_revision;not null" json:"-"`
	Status               string     `gorm:"column:status;size:32;not null" json:"-"`
	ObservedAt           time.Time  `gorm:"column:observed_at;not null" json:"-"`
	FreshUntil           time.Time  `gorm:"column:fresh_until;not null" json:"-"`
	RetryAt              *time.Time `gorm:"column:retry_at" json:"-"`
	CustomerCode         string     `gorm:"column:customer_code;size:64;not null" json:"-"`
	CreatedAt            time.Time  `gorm:"column:created_at;not null" json:"-"`
	UpdatedAt            time.Time  `gorm:"column:updated_at;not null" json:"-"`
}

func (CapabilityProviderHealthSnapshot) TableName() string {
	return "capability_provider_health_snapshots"
}
