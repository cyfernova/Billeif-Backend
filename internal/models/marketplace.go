package models

import (
	"time"
)

type MarketplaceProduct struct {
	ID             string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	AgentID        string    `gorm:"not null;index" json:"agent_id" validate:"required,uuid"`
	ProductID      *string   `gorm:"index" json:"product_id,omitempty" validate:"omitempty,uuid"`
	Name           string    `gorm:"not null;size:255" json:"name" validate:"required,min=2,max=255"`
	Description    *string   `gorm:"type:text" json:"description,omitempty" validate:"omitempty,max=2000"`
	Price          float64   `gorm:"not null;type:decimal(15,2)" json:"price" validate:"required,gte=0"`
	Currency       string    `gorm:"not null;size:3;default:INR" json:"currency" validate:"required,len=3"`
	InventoryCount int       `gorm:"default:0" json:"inventory_count" validate:"gte=0"`
	IsAvailable    bool      `gorm:"default:true;index" json:"is_available"`
	Images         string    `gorm:"type:jsonb;default:'[]'" json:"images"`
	Categories     string    `gorm:"type:jsonb;default:'[]'" json:"categories"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (mp *MarketplaceProduct) TableName() string {
	return "marketplace_products"
}

type MarketplaceOrder struct {
	ID                string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	UserID            string     `gorm:"not null;index" json:"user_id" validate:"required,uuid"`
	ShoppingAgentID   string     `gorm:"not null;index" json:"shopping_agent_id" validate:"required,uuid"`
	MerchantAgentID   string     `gorm:"not null;index" json:"merchant_agent_id" validate:"required,uuid"`
	CartMandateID     string     `gorm:"not null;index" json:"cart_mandate_id" validate:"required,uuid"`
	PaymentMandateID  *string    `gorm:"index" json:"payment_mandate_id,omitempty" validate:"omitempty,uuid"`
	TotalAmount       float64    `gorm:"not null;type:decimal(15,2)" json:"total_amount" validate:"required,gt=0"`
	Currency          string     `gorm:"not null;size:3;default:INR" json:"currency" validate:"required,len=3"`
	Status            string     `gorm:"size:50;default:pending;index" json:"status" validate:"required,oneof=pending confirmed processing shipped delivered cancelled"`
	RazorpayOrderID   *string    `gorm:"size:100" json:"razorpay_order_id,omitempty" validate:"omitempty,max=100"`
	RazorpayPaymentID *string    `gorm:"size:100" json:"razorpay_payment_id,omitempty" validate:"omitempty,max=100"`
	ShippingAddress   *string    `gorm:"type:jsonb" json:"shipping_address,omitempty" validate:"omitempty"`
	TrackingNumber    *string    `gorm:"size:100" json:"tracking_number,omitempty" validate:"omitempty,max=100"`
	EstimatedDelivery *time.Time `json:"estimated_delivery,omitempty"`
	DeliveredAt       *time.Time `json:"delivered_at,omitempty"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`

	CartMandate *CartMandate `gorm:"foreignKey:CartMandateID" json:"cart_mandate,omitempty"`
}

func (mo *MarketplaceOrder) TableName() string {
	return "marketplace_orders"
}

type A2AMessage struct {
	ID              string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	SenderAgentID   *string    `gorm:"index" json:"sender_agent_id,omitempty" validate:"omitempty,uuid"`
	ReceiverAgentID *string    `gorm:"index" json:"receiver_agent_id,omitempty" validate:"omitempty,uuid"`
	MessageType     string     `gorm:"not null;size:100" json:"message_type" validate:"required"`
	Payload         string     `gorm:"type:jsonb;not null" json:"payload" validate:"required"`
	Signature       *string    `gorm:"type:varchar(1000)" json:"signature,omitempty" validate:"omitempty,max=1000"`
	Status          string     `gorm:"size:50;default:pending;index" json:"status" validate:"required,oneof=pending sent delivered failed"`
	Response        *string    `gorm:"type:jsonb" json:"response,omitempty" validate:"omitempty"`
	CreatedAt       time.Time  `gorm:"autoCreateTime;index" json:"created_at"`
	ProcessedAt     *time.Time `json:"processed_at,omitempty"`

	SenderAgent   *Agent `gorm:"foreignKey:SenderAgentID" json:"sender_agent,omitempty"`
	ReceiverAgent *Agent `gorm:"foreignKey:ReceiverAgentID" json:"receiver_agent,omitempty"`
}

func (am *A2AMessage) TableName() string {
	return "a2a_messages"
}
