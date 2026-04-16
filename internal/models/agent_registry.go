package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
)

// AgentRegistry represents an agent in the discovery registry
type AgentRegistry struct {
	ID uuid.UUID `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	// Agent identification
	AgentID          uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"agent_id"`
	AgentName        string    `gorm:"type:varchar(255);not null;index" json:"agent_name"`
	AgentDescription *string   `gorm:"type:text" json:"agent_description"`
	AgentType        string    `gorm:"type:varchar(50);not null;index" json:"agent_type"` // shopping, merchant, credential_provider, payment_processor
	MarketplaceRole  string    `gorm:"-" json:"marketplace_role,omitempty"`

	// Agent Card (AP2 specification) - Full agent card as JSON
	AgentCard datatypes.JSON `gorm:"type:jsonb;not null" json:"agent_card"`

	// Discovery metadata
	Domain       *string `gorm:"type:varchar(255)" json:"domain"`
	WellKnownURI *string `gorm:"type:varchar(500);uniqueIndex" json:"well_known_uri"`
	A2AEndpoint  *string `gorm:"column:a2a_endpoint;type:varchar(500)" json:"a2a_endpoint"`

	// Searchable fields (denormalized for faster queries)
	Capabilities       pq.StringArray `gorm:"type:text[];default:'{}'" json:"capabilities"`
	Tags               pq.StringArray `gorm:"type:text[];default:'{}'" json:"tags"`
	Jurisdictions      pq.StringArray `gorm:"type:text[];default:'{}'" json:"jurisdictions"`
	Currencies         pq.StringArray `gorm:"type:text[];default:'{}'" json:"currencies"`
	SupportedLanguages pq.StringArray `gorm:"type:text[];default:'{}'" json:"supported_languages"`
	ProductIDs         pq.StringArray `gorm:"type:text[];default:'{}'" json:"product_ids"`

	// Products (loaded on demand via productRepo)
	Products []*Product `gorm:"-" json:"products,omitempty"`

	// Pricing information
	PricingModel datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"pricing_model"`

	// Registration metadata
	IsVerified bool `gorm:"default:false;index:idx_agent_registry_verified,priority:1" json:"is_verified"`
	IsActive   bool `gorm:"default:true;index:idx_agent_registry_public,priority:2" json:"is_active"`
	IsPublic   bool `gorm:"default:false;index:idx_agent_registry_public,priority:1" json:"is_public"`

	// Health checking
	LastHealthCheck    *time.Time `gorm:"type:timestamp" json:"last_health_check"`
	HealthCheckStatus  *string    `gorm:"type:varchar(20)" json:"health_check_status"` // 'healthy', 'degraded', 'unhealthy'
	HealthCheckMessage *string    `gorm:"type:text" json:"health_check_message"`

	// Performance metrics (denormalized)
	AverageResponseTimeMs float64 `gorm:"type:float;default:0" json:"average_response_time_ms"`
	SuccessRate           float64 `gorm:"type:float;default:100.0" json:"success_rate"`
	TotalRequests         int     `gorm:"type:int;default:0" json:"total_requests"`
	FailedRequests        int     `gorm:"type:int;default:0" json:"failed_requests"`

	// Ratings and reviews
	AverageRating float64 `gorm:"type:float;default:0" json:"average_rating"`
	TotalReviews  int     `gorm:"type:int;default:0" json:"total_reviews"`

	// Timestamps
	VerifiedAt *time.Time `gorm:"type:timestamp" json:"verified_at"`
	CreatedAt  time.Time  `gorm:"type:timestamp;default:NOW()" json:"created_at"`
	UpdatedAt  time.Time  `gorm:"type:timestamp;default:NOW()" json:"updated_at"`
	DeletedAt  *time.Time `gorm:"type:timestamp;index:idx_agent_registry_verified,priority:2;index:idx_agent_registry_public,priority:3" json:"deleted_at,omitempty"`
}

// TableName specifies the table name for GORM
func (AgentRegistry) TableName() string {
	return "agent_registry"
}

// AgentDiscoveryAudit records changes to agent registry entries
type AgentDiscoveryAudit struct {
	ID uuid.UUID `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	AgentRegistryID uuid.UUID `gorm:"type:uuid;not null;index" json:"agent_registry_id"`

	// Audit information
	Action      string  `gorm:"type:varchar(50);not null;index" json:"action"` // 'registered', 'updated', 'verified', 'deactivated', 'health_check'
	PerformedBy *string `gorm:"type:varchar(50)" json:"performed_by"`          // 'system', 'admin', 'agent'
	Reason      *string `gorm:"type:text" json:"reason"`

	// Previous and current state
	PreviousState datatypes.JSON `gorm:"type:jsonb" json:"previous_state"`
	CurrentState  datatypes.JSON `gorm:"type:jsonb" json:"current_state"`

	// Timestamps
	CreatedAt time.Time `gorm:"type:timestamp;default:NOW();index" json:"created_at"`
}

// TableName specifies the table name for GORM
func (AgentDiscoveryAudit) TableName() string {
	return "agent_discovery_audit"
}

// AgentDiscoveryStats contains analytics for agent discovery
type AgentDiscoveryStats struct {
	ID uuid.UUID `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	AgentRegistryID uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"agent_registry_id"`

	// Discovery metrics
	TotalViews         int `gorm:"type:int;default:0" json:"total_views"`
	TotalSearchesFound int `gorm:"type:int;default:0" json:"total_searches_found"`
	TotalInquiries     int `gorm:"type:int;default:0" json:"total_inquiries"`
	TotalIntegrations  int `gorm:"type:int;default:0" json:"total_integrations"`

	// Recent activity
	LastViewedAt      *time.Time `gorm:"type:timestamp" json:"last_viewed_at"`
	LastSearchFoundAt *time.Time `gorm:"type:timestamp" json:"last_search_found_at"`
	LastInquiryAt     *time.Time `gorm:"type:timestamp" json:"last_inquiry_at"`
	LastIntegrationAt *time.Time `gorm:"type:timestamp" json:"last_integration_at"`

	// Weekly stats
	ViewsThisWeek        int `gorm:"type:int;default:0" json:"views_this_week"`
	InquiriesThisWeek    int `gorm:"type:int;default:0" json:"inquiries_this_week"`
	IntegrationsThisWeek int `gorm:"type:int;default:0" json:"integrations_this_week"`

	// Timestamps
	CreatedAt time.Time `gorm:"type:timestamp;default:NOW()" json:"created_at"`
	UpdatedAt time.Time `gorm:"type:timestamp;default:NOW()" json:"updated_at"`
}

// TableName specifies the table name for GORM
func (AgentDiscoveryStats) TableName() string {
	return "agent_discovery_stats"
}

// AgentCard represents the AP2 Agent Card specification
type AgentCard struct {
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	Endpoint         string                 `json:"endpoint"`
	Type             string                 `json:"type"` // shopping, merchant, credential_provider, payment_processor
	MarketplaceRole  string                 `json:"marketplace_role,omitempty"`
	Capabilities     []string               `json:"capabilities"`
	PricingModel     map[string]interface{} `json:"pricing_model"`
	Currencies       []string               `json:"currencies"`
	Jurisdictions    []string               `json:"jurisdictions"`
	Languages        []string               `json:"languages"`
	SupportedMethods []string               `json:"supported_methods"`    // payment methods, product categories, etc.
	Version          string                 `json:"version"`              // AP2 spec version
	PublicKey        *string                `json:"public_key,omitempty"` // Agent's public key for signature verification
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// Scan implements sql.Scanner interface
func (ac *AgentCard) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}
	return json.Unmarshal(bytes, &ac)
}

// Value implements driver.Valuer interface
func (ac AgentCard) Value() (driver.Value, error) {
	return json.Marshal(ac)
}

// PricingModel represents pricing information for an agent
type AgentPricingModel struct {
	Commission        *float64 `json:"commission,omitempty"`          // Percentage commission
	MonthlyFee        *float64 `json:"monthly_fee,omitempty"`         // Fixed monthly fee
	PerTransactionFee *float64 `json:"per_transaction_fee,omitempty"` // Fee per transaction
	Currency          string   `json:"currency"`                      // Currency for pricing
	MinimumOrder      *float64 `json:"minimum_order,omitempty"`       // Minimum order value
	Description       string   `json:"description,omitempty"`
}

// AgentSearchQuery represents a search query for discovering agents
type AgentSearchQuery struct {
	Query         string   `json:"query"`         // Free text search
	Types         []string `json:"types"`         // Filter by agent types
	Capabilities  []string `json:"capabilities"`  // Filter by capabilities
	Tags          []string `json:"tags"`          // Filter by tags
	Jurisdictions []string `json:"jurisdictions"` // Filter by jurisdictions
	Currencies    []string `json:"currencies"`    // Filter by supported currencies
	Languages     []string `json:"languages"`     // Filter by supported languages
	MinRating     *float64 `json:"min_rating"`    // Minimum rating
	OnlyVerified  bool     `json:"only_verified"` // Only verified agents
	OnlyPublic    bool     `json:"only_public"`   // Only public agents
	Page          int      `json:"page"`          // Pagination page
	Limit         int      `json:"limit"`         // Results per page
	SortBy        string   `json:"sort_by"`       // Sort field: "rating", "views", "created", "updated"
	SortOrder     string   `json:"sort_order"`    // "asc" or "desc"
}

// AgentDiscoveryFilter helps with advanced filtering
type AgentDiscoveryFilter struct {
	BusinessID        string
	IDs               []uuid.UUID
	AgentTypes        []string
	Capabilities      []string
	Tags              []string
	Jurisdictions     []string
	Currencies        []string
	Languages         []string
	IsVerified        *bool
	IsPublic          *bool
	IsActive          *bool
	MinAverageRating  *float64
	MaxAverageRating  *float64
	MinTotalReviews   *int
	HealthCheckStatus *string
	ExcludeDeleted    bool
}

// HealthCheckResult represents a health check for an agent
type HealthCheckResult struct {
	AgentRegistryID uuid.UUID
	Status          string // 'healthy', 'degraded', 'unhealthy'
	ResponseTimeMs  float64
	Message         string
	Timestamp       time.Time
	IsSuccessful    bool
}
