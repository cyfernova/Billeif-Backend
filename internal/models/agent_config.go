package models

import "time"

type AgentConfig struct {
	Type         AgentType     `json:"type"`
	Volatility   float64       `json:"volatility"`
	BuyerConfig  *BuyerConfig  `json:"buyer_config,omitempty"`
	SellerConfig *SellerConfig `json:"seller_config,omitempty"`
}

type AgentType string

const (
	AgentTypeBuyer  AgentType = "buyer"
	AgentTypeSeller AgentType = "seller"
)

type BuyerConfig struct {
	MaxDiscountPercent  float64       `json:"max_discount_percent"`
	MinDiscountPercent  float64       `json:"min_discount_percent"`
	TargetDiscount      float64       `json:"target_discount"`
	RiskTolerance       float64       `json:"risk_tolerance"`
	PatienceLevel       float64       `json:"patience_level"`
	MaxRounds           int           `json:"max_rounds"`
	PreferredProducts   []string      `json:"preferred_products"`
	BlacklistedVendors  []string      `json:"blacklisted_vendors"`
	BudgetLimit         *float64      `json:"budget_limit,omitempty"`
	PaymentTerms        []string      `json:"payment_terms"`
	DeliveryPreferences *DeliveryPref `json:"delivery_preferences,omitempty"`
	AcceptanceThreshold float64       `json:"acceptance_threshold"`
}

type SellerConfig struct {
	MinAcceptablePrice    float64        `json:"min_acceptable_price"`
	MaxMarkupPercent      float64        `json:"max_markup_percent"`
	InventoryPressure     float64        `json:"inventory_pressure"`
	SalesVolumeGoal       *float64       `json:"sales_volume_goal,omitempty"`
	CustomerLoyaltyFactor float64        `json:"customer_loyalty_factor"`
	MaxRounds             int            `json:"max_rounds"`
	PreferredCustomers    []string       `json:"preferred_customers"`
	VolumeDiscountTiers   []DiscountTier `json:"volume_discount_tiers"`
	PaymentTerms          []string       `json:"payment_terms"`
	AcceptanceThreshold   float64        `json:"acceptance_threshold"`
	SeasonalAdjustments   *SeasonalAdj   `json:"seasonal_adjustments,omitempty"`
}

type DiscountTier struct {
	MinQuantity     int     `json:"min_quantity"`
	DiscountPercent float64 `json:"discount_percent"`
}

type DeliveryPref struct {
	MaxDeliveryDays   int      `json:"max_delivery_days"`
	PreferredCarriers []string `json:"preferred_carriers"`
	TrackingRequired  bool     `json:"tracking_required"`
	InsuranceRequired bool     `json:"insurance_required"`
}

type SeasonalAdj struct {
	Multiplier    float64   `json:"multiplier"`
	Season        string    `json:"season"`
	EffectiveFrom time.Time `json:"effective_from"`
	EffectiveTo   time.Time `json:"effective_to"`
}

type WellKnownAgentConfig struct {
	AgentID      string                 `json:"agent_id"`
	Name         string                 `json:"name"`
	Type         AgentType              `json:"type"`
	Description  string                 `json:"description"`
	Version      string                 `json:"version"`
	Config       *AgentConfig           `json:"config"`
	Capabilities []string               `json:"capabilities"`
	A2AEndpoint  string                 `json:"a2a_endpoint"`
	PublicKey    *string                `json:"public_key,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

type WellKnownAgentsFile struct {
	Version  string                 `json:"version"`
	Agents   []WellKnownAgentConfig `json:"agents"`
	LastSync time.Time              `json:"last_sync"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}
