package models

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

type Agent struct {
	ID              string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OwnerID         string         `gorm:"not null;index" json:"owner_id" validate:"required,uuid"`
	BusinessID      string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Name            string         `gorm:"not null;size:255" json:"name" validate:"required,min=2,max=255"`
	Type            string         `gorm:"not null;size:50;index" json:"type" validate:"required,oneof=buyer seller shopping merchant credential_provider payment_processor"`
	Description     *string        `gorm:"type:text" json:"description,omitempty" validate:"omitempty,max=1000"`
	Capabilities    string         `gorm:"type:jsonb;not null;default:'[]'" json:"capabilities"`
	Categories      pq.StringArray `gorm:"type:text[];default:'{}'" json:"categories"`
	Config          string         `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	A2AEndpoint     *string        `gorm:"column:a2a_endpoint;type:varchar(500)" json:"a2a_endpoint,omitempty" validate:"omitempty,url,max=500"`
	MarketplaceRole string         `gorm:"-" json:"marketplace_role,omitempty"`
	IsPublic        bool           `gorm:"default:false" json:"is_public"`
	IsActive        bool           `gorm:"default:true;index" json:"is_active"`
	CreatedAt       time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
	ProductIDs      []string       `gorm:"-" json:"product_ids,omitempty"`

	AgentCapabilities   []AgentCapability    `gorm:"foreignKey:AgentID" json:"agent_capabilities,omitempty"`
	AgentTransactions   []AgentTransaction   `gorm:"foreignKey:AgentID" json:"agent_transactions,omitempty"`
	MarketplaceProducts []MarketplaceProduct `gorm:"foreignKey:AgentID" json:"marketplace_products,omitempty"`
}

func (a *Agent) TableName() string {
	return "agents"
}

type AgentCapability struct {
	ID             string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	AgentID        string    `gorm:"not null;index" json:"agent_id" validate:"required,uuid"`
	CapabilityType string    `gorm:"not null;size:100" json:"capability_type" validate:"required"`
	Description    *string   `gorm:"type:text" json:"description,omitempty" validate:"omitempty,max=500"`
	Config         string    `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (ac *AgentCapability) TableName() string {
	return "agent_capabilities"
}

type AgentTransaction struct {
	ID              string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	AgentID         string    `gorm:"not null;index" json:"agent_id" validate:"required,uuid"`
	UserID          string    `gorm:"not null;index" json:"user_id" validate:"required,uuid"`
	MandateID       string    `gorm:"not null" json:"mandate_id" validate:"required,uuid"`
	TransactionType string    `gorm:"not null;size:50" json:"transaction_type" validate:"required,oneof=intent cart payment"`
	Status          string    `gorm:"not null;size:50;default:pending" json:"status" validate:"required"`
	Metadata        string    `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`
	CreatedAt       time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (at *AgentTransaction) TableName() string {
	return "agent_transactions"
}
