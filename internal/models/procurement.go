package models

import (
	"time"

	"gorm.io/gorm"
)

type ProcurementRun struct {
	ID                  string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	UserID              string         `gorm:"not null;index" json:"user_id" validate:"required,uuid"`
	ShoppingAgentID     string         `gorm:"not null;index" json:"shopping_agent_id" validate:"required,uuid"`
	Intent              string         `gorm:"type:text;not null" json:"intent" validate:"required,max=2000"`
	Quantity            int            `gorm:"not null;default:1" json:"quantity" validate:"gte=1"`
	MaxBudget           float64        `gorm:"not null;type:decimal(15,2)" json:"max_budget" validate:"required,gt=0"`
	Currency            string         `gorm:"not null;size:3;default:INR" json:"currency" validate:"required,len=3"`
	PaymentTerms        string         `gorm:"type:jsonb;not null;default:'[]'" json:"payment_terms"`
	AutoBuy             bool           `gorm:"default:true" json:"auto_buy"`
	MaxSellers          int            `gorm:"not null;default:5" json:"max_sellers" validate:"gte=1,lte=20"`
	Status              string         `gorm:"not null;size:50;default:pending;index" json:"status"`
	IdempotencyKey      *string        `gorm:"size:255;uniqueIndex:idx_procurement_runs_idempotency" json:"idempotency_key,omitempty"`
	IntentMandateID     *string        `gorm:"index" json:"intent_mandate_id,omitempty" validate:"omitempty,uuid"`
	WinningCandidateID  *string        `gorm:"index" json:"winning_candidate_id,omitempty" validate:"omitempty,uuid"`
	WinningNegotiationID *string       `gorm:"index" json:"winning_negotiation_id,omitempty" validate:"omitempty,uuid"`
	CartMandateID       *string        `gorm:"index" json:"cart_mandate_id,omitempty" validate:"omitempty,uuid"`
	PaymentMandateID    *string        `gorm:"index" json:"payment_mandate_id,omitempty" validate:"omitempty,uuid"`
	OrderID             *string        `gorm:"index" json:"order_id,omitempty" validate:"omitempty,uuid"`
	FailureReason       *string        `gorm:"type:text" json:"failure_reason,omitempty"`
	Metadata            string         `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`
	CreatedAt           time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt           time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	CancelledAt         *time.Time     `json:"cancelled_at,omitempty"`
	DeletedAt           gorm.DeletedAt `gorm:"index" json:"-"`

	Candidates []ProcurementCandidate `gorm:"foreignKey:ProcurementRunID" json:"candidates,omitempty"`
}

func (ProcurementRun) TableName() string {
	return "procurement_runs"
}

type ProcurementCandidate struct {
	ID                 string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ProcurementRunID   string         `gorm:"not null;index" json:"procurement_run_id" validate:"required,uuid"`
	MerchantAgentID    string         `gorm:"not null;index" json:"merchant_agent_id" validate:"required,uuid"`
	MarketplaceProductID string       `gorm:"not null;index" json:"marketplace_product_id" validate:"required,uuid"`
	MatchScore         float64        `gorm:"type:float;default:0" json:"match_score"`
	EligibilityStatus  string         `gorm:"not null;size:50;default:eligible;index" json:"eligibility_status"`
	NegotiationID      *string        `gorm:"index" json:"negotiation_id,omitempty" validate:"omitempty,uuid"`
	FinalAmount        *float64       `gorm:"type:decimal(15,2)" json:"final_amount,omitempty"`
	TerminalReason     *string        `gorm:"type:text" json:"terminal_reason,omitempty"`
	SelectionReason    *string        `gorm:"type:text" json:"selection_reason,omitempty"`
	ReservedQuantity   int            `gorm:"not null;default:0" json:"reserved_quantity" validate:"gte=0"`
	Metadata           string         `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`
	CreatedAt          time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`

	ProcurementRun   *ProcurementRun   `gorm:"foreignKey:ProcurementRunID" json:"procurement_run,omitempty"`
	MerchantAgent    *Agent            `gorm:"foreignKey:MerchantAgentID" json:"merchant_agent,omitempty"`
	MarketplaceProduct *MarketplaceProduct `gorm:"foreignKey:MarketplaceProductID" json:"marketplace_product,omitempty"`
}

func (ProcurementCandidate) TableName() string {
	return "procurement_candidates"
}
