package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	StorefrontStatusDraft     = "draft"
	StorefrontStatusPublished = "published"
	StorefrontStatusArchived  = "archived"
)

const (
	StoreOrderStatusPending            = "pending"
	StoreOrderStatusAwaitingApproval   = "awaiting_approval"
	StoreOrderStatusConfirmed          = "confirmed"
	StoreOrderStatusPaid               = "paid"
	StoreOrderStatusCancelled          = "cancelled"
	StoreOrderStatusPaymentFailed      = "payment_failed"
	StoreOrderStatusPartiallyFulfilled = "partially_fulfilled"
)

const (
	StoreOrderPaymentStatusPending  = "pending"
	StoreOrderPaymentStatusCOD      = "cod"
	StoreOrderPaymentStatusPaid     = "paid"
	StoreOrderPaymentStatusFailed   = "failed"
	StoreOrderPaymentStatusRefunded = "refunded"
)

const (
	StoreCouponDiscountTypePercent = "percentage"
	StoreCouponDiscountTypeFixed   = "fixed"
)

const (
	NotificationChannelWhatsApp = "whatsapp"
)

type FeatureEntitlement struct {
	ID         string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID string         `gorm:"not null;index" json:"business_id"`
	FeatureKey string         `gorm:"not null;size:120;index" json:"feature_key"`
	Enabled    bool           `gorm:"not null;default:false" json:"enabled"`
	LimitValue *int64         `json:"limit_value,omitempty"`
	Metadata   string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

func (FeatureEntitlement) TableName() string {
	return "feature_entitlements"
}

type Role struct {
	ID          string            `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID  string            `gorm:"not null;index" json:"business_id"`
	Name        string            `gorm:"not null;size:120" json:"name"`
	Key         string            `gorm:"not null;size:120;index" json:"key"`
	Description string            `gorm:"type:text" json:"description,omitempty"`
	IsSystem    bool              `gorm:"not null;default:false" json:"is_system"`
	Metadata    string            `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt   time.Time         `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time         `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt    `gorm:"index" json:"-"`
	Permissions []*RolePermission `gorm:"foreignKey:RoleID" json:"permissions,omitempty"`
}

func (Role) TableName() string {
	return "roles"
}

type RolePermission struct {
	ID            string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	RoleID        string         `gorm:"not null;index" json:"role_id"`
	PermissionKey string         `gorm:"not null;size:150;index" json:"permission_key"`
	CreatedAt     time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

func (RolePermission) TableName() string {
	return "role_permissions"
}

type Branch struct {
	ID         string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID string         `gorm:"not null;index" json:"business_id"`
	Name       string         `gorm:"not null;size:160" json:"name"`
	Code       string         `gorm:"not null;size:60;index" json:"code"`
	Email      string         `gorm:"size:255" json:"email,omitempty"`
	Phone      string         `gorm:"size:50" json:"phone,omitempty"`
	Address    string         `gorm:"size:500" json:"address,omitempty"`
	City       string         `gorm:"size:100" json:"city,omitempty"`
	State      string         `gorm:"size:100" json:"state,omitempty"`
	Country    string         `gorm:"size:100" json:"country,omitempty"`
	PostalCode string         `gorm:"size:20" json:"postal_code,omitempty"`
	IsDefault  bool           `gorm:"not null;default:false" json:"is_default"`
	Metadata   string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Branch) TableName() string {
	return "branches"
}

type Storefront struct {
	ID                 string              `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID         string              `gorm:"not null;index" json:"business_id"`
	Name               string              `gorm:"not null;size:160" json:"name"`
	Slug               string              `gorm:"not null;size:160;index" json:"slug"`
	Status             string              `gorm:"not null;size:30;default:'draft';index" json:"status"`
	Currency           string              `gorm:"not null;size:3;default:'INR'" json:"currency"`
	AllowCOD           bool                `gorm:"not null;default:true" json:"allow_cod"`
	AllowOnlinePayment bool                `gorm:"not null;default:false" json:"allow_online_payment"`
	AutoInvoiceOnPaid  bool                `gorm:"not null;default:true" json:"auto_invoice_on_paid"`
	MinimumOrderValue  float64             `gorm:"type:decimal(15,2);default:0" json:"minimum_order_value"`
	Settings           string              `gorm:"type:jsonb;default:'{}'" json:"settings,omitempty"`
	BlockedUsers       string              `gorm:"type:jsonb;default:'[]'" json:"blocked_users,omitempty"`
	CreatedAt          time.Time           `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time           `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt          gorm.DeletedAt      `gorm:"index" json:"-"`
	Domains            []*StorefrontDomain `gorm:"foreignKey:StorefrontID" json:"domains,omitempty"`
}

func (Storefront) TableName() string {
	return "storefronts"
}

type StorefrontDomain struct {
	ID           string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	StorefrontID string         `gorm:"not null;index" json:"storefront_id"`
	Domain       string         `gorm:"not null;size:255;index" json:"domain"`
	IsPrimary    bool           `gorm:"not null;default:false" json:"is_primary"`
	VerifiedAt   *time.Time     `json:"verified_at,omitempty"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (StorefrontDomain) TableName() string {
	return "storefront_domains"
}

type StorefrontCategory struct {
	ID           string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	StorefrontID string         `gorm:"not null;index" json:"storefront_id"`
	Name         string         `gorm:"not null;size:160" json:"name"`
	Slug         string         `gorm:"not null;size:160;index" json:"slug"`
	Description  string         `gorm:"type:text" json:"description,omitempty"`
	ImageURL     string         `gorm:"size:500" json:"image_url,omitempty"`
	SortOrder    int            `gorm:"not null;default:0" json:"sort_order"`
	SEO          string         `gorm:"type:jsonb;default:'{}'" json:"seo,omitempty"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (StorefrontCategory) TableName() string {
	return "storefront_categories"
}

type StorefrontProduct struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	StorefrontID   string         `gorm:"not null;index" json:"storefront_id"`
	ProductID      string         `gorm:"not null;index" json:"product_id"`
	CategoryID     *string        `gorm:"index" json:"category_id,omitempty"`
	IsPublished    bool           `gorm:"not null;default:false" json:"is_published"`
	DisplayPrice   float64        `gorm:"type:decimal(15,2);default:0" json:"display_price"`
	CompareAtPrice float64        `gorm:"type:decimal(15,2);default:0" json:"compare_at_price"`
	SortOrder      int            `gorm:"not null;default:0" json:"sort_order"`
	Badge          string         `gorm:"size:80" json:"badge,omitempty"`
	SEO            string         `gorm:"type:jsonb;default:'{}'" json:"seo,omitempty"`
	Metadata       string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (StorefrontProduct) TableName() string {
	return "storefront_products"
}

type StorefrontCoupon struct {
	ID                    string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	StorefrontID          string         `gorm:"not null;index" json:"storefront_id"`
	Code                  string         `gorm:"not null;size:80;index" json:"code"`
	DiscountType          string         `gorm:"not null;size:30" json:"discount_type"`
	DiscountValue         float64        `gorm:"type:decimal(15,2);default:0" json:"discount_value"`
	MinimumOrderValue     float64        `gorm:"type:decimal(15,2);default:0" json:"minimum_order_value"`
	MaxDiscountAmount     float64        `gorm:"type:decimal(15,2);default:0" json:"max_discount_amount"`
	UsageLimit            int64          `gorm:"not null;default:0" json:"usage_limit"`
	UsageLimitPerCustomer int64          `gorm:"not null;default:0" json:"usage_limit_per_customer"`
	StartsAt              *time.Time     `json:"starts_at,omitempty"`
	EndsAt                *time.Time     `json:"ends_at,omitempty"`
	IsActive              bool           `gorm:"not null;default:true" json:"is_active"`
	Metadata              string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt             time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt             time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt             gorm.DeletedAt `gorm:"index" json:"-"`
}

func (StorefrontCoupon) TableName() string {
	return "storefront_coupons"
}

type StorefrontCouponRedemption struct {
	ID                 string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	StorefrontCouponID string         `gorm:"not null;index" json:"storefront_coupon_id"`
	StoreOrderID       *string        `gorm:"index" json:"store_order_id,omitempty"`
	CustomerID         *string        `gorm:"index" json:"customer_id,omitempty"`
	CustomerEmail      string         `gorm:"size:255;index" json:"customer_email,omitempty"`
	DiscountAmount     float64        `gorm:"type:decimal(15,2);default:0" json:"discount_amount"`
	CreatedAt          time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
}

func (StorefrontCouponRedemption) TableName() string {
	return "storefront_coupon_redemptions"
}

type StoreOrder struct {
	ID                 string             `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID         string             `gorm:"not null;index" json:"business_id"`
	StorefrontID       string             `gorm:"not null;index" json:"storefront_id"`
	BranchID           *string            `gorm:"index" json:"branch_id,omitempty"`
	CustomerID         *string            `gorm:"index" json:"customer_id,omitempty"`
	CouponID           *string            `gorm:"index" json:"coupon_id,omitempty"`
	SalesOrderID       *string            `gorm:"index" json:"sales_order_id,omitempty"`
	SalesInvoiceID     *string            `gorm:"index" json:"sales_invoice_id,omitempty"`
	PublicToken        string             `gorm:"not null;size:120;index" json:"public_token"`
	OrderNumber        string             `gorm:"not null;size:80;index" json:"order_number"`
	Status             string             `gorm:"not null;size:40;default:'pending';index" json:"status"`
	PaymentStatus      string             `gorm:"not null;size:40;default:'pending';index" json:"payment_status"`
	PaymentMethod      string             `gorm:"size:40" json:"payment_method,omitempty"`
	Currency           string             `gorm:"not null;size:3;default:'INR'" json:"currency"`
	ExchangeRate       float64            `gorm:"type:decimal(18,6);default:1" json:"exchange_rate"`
	FXProvider         string             `gorm:"size:80" json:"fx_provider,omitempty"`
	FXBaseCurrency     string             `gorm:"size:3" json:"fx_base_currency,omitempty"`
	FXQuoteCurrency    string             `gorm:"size:3" json:"fx_quote_currency,omitempty"`
	FXRateTimestamp    *time.Time         `json:"fx_rate_timestamp,omitempty"`
	Subtotal           float64            `gorm:"type:decimal(15,2);default:0" json:"subtotal"`
	DiscountTotal      float64            `gorm:"type:decimal(15,2);default:0" json:"discount_total"`
	TaxTotal           float64            `gorm:"type:decimal(15,2);default:0" json:"tax_total"`
	ShippingTotal      float64            `gorm:"type:decimal(15,2);default:0" json:"shipping_total"`
	Total              float64            `gorm:"type:decimal(15,2);default:0" json:"total"`
	Snapshot           string             `gorm:"type:jsonb;default:'{}'" json:"snapshot,omitempty"`
	BillingAddress     string             `gorm:"type:jsonb;default:'{}'" json:"billing_address,omitempty"`
	ShippingAddress    string             `gorm:"type:jsonb;default:'{}'" json:"shipping_address,omitempty"`
	Notes              string             `gorm:"type:text" json:"notes,omitempty"`
	IdempotencyKey     string             `gorm:"size:255;index" json:"idempotency_key,omitempty"`
	ExternalOrderID    string             `gorm:"size:160" json:"external_order_id,omitempty"`
	ExternalPaymentID  string             `gorm:"size:160" json:"external_payment_id,omitempty"`
	GatewayOrderID     string             `gorm:"size:160" json:"gateway_order_id,omitempty"`
	GatewayPaymentID   string             `gorm:"size:160" json:"gateway_payment_id,omitempty"`
	WebhookReference   string             `gorm:"size:160" json:"webhook_reference,omitempty"`
	OrderedAt          time.Time          `gorm:"not null;default:now()" json:"ordered_at"`
	PaidAt             *time.Time         `json:"paid_at,omitempty"`
	CancelledAt        *time.Time         `json:"cancelled_at,omitempty"`
	CancellationReason string             `gorm:"type:text" json:"cancellation_reason,omitempty"`
	CreatedAt          time.Time          `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time          `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt          gorm.DeletedAt     `gorm:"index" json:"-"`
	Lines              []*StoreOrderLine  `gorm:"foreignKey:StoreOrderID" json:"lines,omitempty"`
	Events             []*StoreOrderEvent `gorm:"foreignKey:StoreOrderID" json:"events,omitempty"`
}

func (StoreOrder) TableName() string {
	return "store_orders"
}

type StoreOrderLine struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	StoreOrderID   string         `gorm:"not null;index" json:"store_order_id"`
	ProductID      *string        `gorm:"index" json:"product_id,omitempty"`
	VariantID      *string        `gorm:"index" json:"variant_id,omitempty"`
	WarehouseID    *string        `gorm:"index" json:"warehouse_id,omitempty"`
	Title          string         `gorm:"not null;size:255" json:"title"`
	SKU            string         `gorm:"size:100" json:"sku,omitempty"`
	Quantity       float64        `gorm:"type:decimal(15,3);not null;default:0" json:"quantity"`
	UnitPrice      float64        `gorm:"type:decimal(15,2);default:0" json:"unit_price"`
	DiscountAmount float64        `gorm:"type:decimal(15,2);default:0" json:"discount_amount"`
	TaxRate        float64        `gorm:"type:decimal(7,3);default:0" json:"tax_rate"`
	TaxAmount      float64        `gorm:"type:decimal(15,2);default:0" json:"tax_amount"`
	LineTotal      float64        `gorm:"type:decimal(15,2);default:0" json:"line_total"`
	Snapshot       string         `gorm:"type:jsonb;default:'{}'" json:"snapshot,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (StoreOrderLine) TableName() string {
	return "store_order_lines"
}

type StoreOrderEvent struct {
	ID           string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	StoreOrderID string         `gorm:"not null;index" json:"store_order_id"`
	EventType    string         `gorm:"not null;size:80;index" json:"event_type"`
	Status       string         `gorm:"size:40" json:"status,omitempty"`
	Payload      string         `gorm:"type:jsonb;default:'{}'" json:"payload,omitempty"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (StoreOrderEvent) TableName() string {
	return "store_order_events"
}

type FXRate struct {
	ID            string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	Provider      string         `gorm:"not null;size:80;index" json:"provider"`
	BaseCurrency  string         `gorm:"not null;size:3;index" json:"base_currency"`
	QuoteCurrency string         `gorm:"not null;size:3;index" json:"quote_currency"`
	Rate          float64        `gorm:"type:decimal(18,6);not null;default:1" json:"rate"`
	FetchedAt     time.Time      `gorm:"not null;index" json:"fetched_at"`
	Metadata      string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt     time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

func (FXRate) TableName() string {
	return "fx_rates"
}

type DriveAsset struct {
	ID          string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID  string         `gorm:"not null;index" json:"business_id"`
	UploadedBy  *string        `gorm:"index" json:"uploaded_by,omitempty"`
	Name        string         `gorm:"not null;size:255" json:"name"`
	Bucket      string         `gorm:"not null;size:255" json:"bucket"`
	ObjectKey   string         `gorm:"not null;size:500;index" json:"object_key"`
	ContentType string         `gorm:"size:120" json:"content_type,omitempty"`
	SizeBytes   int64          `gorm:"not null;default:0" json:"size_bytes"`
	Category    string         `gorm:"size:80" json:"category,omitempty"`
	Metadata    string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (DriveAsset) TableName() string {
	return "drive_assets"
}

type WhatsAppConfig struct {
	ID               string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID       string         `gorm:"not null;index" json:"business_id"`
	PhoneNumberID    string         `gorm:"size:120" json:"phone_number_id,omitempty"`
	AccessToken      string         `gorm:"type:text" json:"access_token,omitempty"`
	WebhookSecret    string         `gorm:"type:text" json:"webhook_secret,omitempty"`
	VerifyToken      string         `gorm:"size:255" json:"verify_token,omitempty"`
	DefaultRecipient string         `gorm:"size:50" json:"default_recipient,omitempty"`
	Enabled          bool           `gorm:"not null;default:false" json:"enabled"`
	Metadata         string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt        time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (WhatsAppConfig) TableName() string {
	return "whatsapp_configs"
}

type NotificationTemplate struct {
	ID           string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID   string         `gorm:"not null;index" json:"business_id"`
	Channel      string         `gorm:"not null;size:40;index" json:"channel"`
	EventKey     string         `gorm:"not null;size:120;index" json:"event_key"`
	Name         string         `gorm:"not null;size:160" json:"name"`
	LanguageCode string         `gorm:"size:20;default:'en'" json:"language_code,omitempty"`
	Body         string         `gorm:"type:text" json:"body"`
	Enabled      bool           `gorm:"not null;default:true" json:"enabled"`
	Metadata     string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (NotificationTemplate) TableName() string {
	return "notification_templates"
}

type NotificationDelivery struct {
	ID                string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID        string         `gorm:"not null;index" json:"business_id"`
	StoreOrderID      *string        `gorm:"index" json:"store_order_id,omitempty"`
	Channel           string         `gorm:"not null;size:40;index" json:"channel"`
	EventKey          string         `gorm:"not null;size:120;index" json:"event_key"`
	Recipient         string         `gorm:"size:120" json:"recipient,omitempty"`
	Status            string         `gorm:"not null;size:40;default:'queued';index" json:"status"`
	RequestPayload    string         `gorm:"type:jsonb;default:'{}'" json:"request_payload,omitempty"`
	ResponsePayload   string         `gorm:"type:jsonb;default:'{}'" json:"response_payload,omitempty"`
	ExternalMessageID string         `gorm:"size:160" json:"external_message_id,omitempty"`
	AttemptCount      int            `gorm:"not null;default:0" json:"attempt_count"`
	LastAttemptAt     *time.Time     `json:"last_attempt_at,omitempty"`
	DeliveredAt       *time.Time     `json:"delivered_at,omitempty"`
	CreatedAt         time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

func (NotificationDelivery) TableName() string {
	return "notification_deliveries"
}
