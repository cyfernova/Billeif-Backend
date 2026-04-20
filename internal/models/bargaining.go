package models

import (
	"time"
)

type BargainingNegotiation struct {
	ID                 string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	SessionID          *string    `gorm:"index" json:"session_id,omitempty"`
	BuyerAgentID       string     `gorm:"not null;index" json:"buyer_agent_id" validate:"required,uuid"`
	SellerAgentID      string     `gorm:"not null;index" json:"seller_agent_id" validate:"required,uuid"`
	UserID             string     `gorm:"not null;index" json:"user_id" validate:"required,uuid"`
	MarketplaceOrderID *string    `gorm:"index" json:"marketplace_order_id,omitempty" validate:"omitempty,uuid"`
	InitialAmount      float64    `gorm:"not null;type:decimal(15,2)" json:"initial_amount" validate:"required,gt=0"`
	CurrentAmount      float64    `gorm:"not null;type:decimal(15,2)" json:"current_amount" validate:"required,gte=0"`
	BuyerVolatility    float64    `gorm:"not null;type:decimal(3,2)" json:"buyer_volatility" validate:"required,gte=0,lte=1"`
	SellerVolatility   float64    `gorm:"not null;type:decimal(3,2)" json:"seller_volatility" validate:"required,gte=0,lte=1"`
	Status             string     `gorm:"not null;size:50;default:initiated;index" json:"status" validate:"required,oneof=initiated in_progress accepted rejected expired"`
	Rounds             int        `gorm:"default:0" json:"rounds" validate:"gte=0"`
	MaxRounds          int        `gorm:"default:5" json:"max_rounds" validate:"required,gte=1,lte=20"`
	ExpiresAt          time.Time  `gorm:"not null;index" json:"expires_at"`
	Metadata           string     `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`
	CreatedAt          time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`

	BuyerAgent  *Agent `gorm:"foreignKey:BuyerAgentID" json:"buyer_agent,omitempty"`
	SellerAgent *Agent `gorm:"foreignKey:SellerAgentID" json:"seller_agent,omitempty"`
}

func (bn *BargainingNegotiation) TableName() string {
	return "bargaining_negotiations"
}

type BargainingRound struct {
	ID               string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	NegotiationID    string    `gorm:"not null;index" json:"negotiation_id" validate:"required,uuid"`
	AgentID          string    `gorm:"not null;index" json:"agent_id" validate:"required,uuid"`
	RoundNumber      int       `gorm:"not null" json:"round_number" validate:"required,gte=1"`
	ProposedAmount   float64   `gorm:"not null;type:decimal(15,2)" json:"proposed_amount" validate:"required,gte=0"`
	PreviousAmount   float64   `gorm:"not null;type:decimal(15,2)" json:"previous_amount" validate:"required,gte=0"`
	AgentType        string    `gorm:"not null;size:10" json:"agent_type" validate:"required,oneof=buyer seller"`
	Action           string    `gorm:"not null;size:20" json:"action" validate:"required,oneof=counteroffer accept reject"`
	Reason           *string   `gorm:"type:text" json:"reason,omitempty" validate:"omitempty,max=500"`
	VolatilityFactor float64   `gorm:"type:decimal(5,4)" json:"volatility_factor" validate:"omitempty,gte=0,lte=1"`
	Metadata         string    `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`
	CreatedAt        time.Time `gorm:"autoCreateTime" json:"created_at"`

	Negotiation *BargainingNegotiation `gorm:"foreignKey:NegotiationID" json:"negotiation,omitempty"`
	Agent       *Agent                 `gorm:"foreignKey:AgentID" json:"agent,omitempty"`
}

func (br *BargainingRound) TableName() string {
	return "bargaining_rounds"
}
