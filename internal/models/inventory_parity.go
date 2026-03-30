package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	InventoryTransactionTypeOpeningBalance = "opening_balance"
	InventoryTransactionTypeStockIn        = "stock_in"
	InventoryTransactionTypeStockOut       = "stock_out"
	InventoryTransactionTypeAdjustment     = "adjustment"
	InventoryTransactionTypeTransfer       = "transfer"
	InventoryTransactionTypeReservation    = "reservation"
	InventoryTransactionTypeRelease        = "release"
	InventoryTransactionTypeReset          = "reset"
	InventoryTransactionTypeAssembly       = "assembly"
	InventoryTransactionTypeDisassembly    = "disassembly"
)

const (
	SerialStatusAvailable = "available"
	SerialStatusAllocated = "allocated"
	SerialStatusSold      = "sold"
	SerialStatusConsumed  = "consumed"
	SerialStatusArchived  = "archived"
)

type ProductCategory struct {
	ID         string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	ParentID   *string        `gorm:"index" json:"parent_id,omitempty" validate:"omitempty,uuid"`
	Name       string         `gorm:"not null;size:120" json:"name"`
	Slug       string         `gorm:"not null;size:140" json:"slug"`
	SortOrder  int            `gorm:"default:0" json:"sort_order"`
	IsActive   bool           `gorm:"default:true;index" json:"is_active"`
	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ProductCategory) TableName() string {
	return "product_categories"
}

type ProductImage struct {
	ID         string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	ProductID  string         `gorm:"not null;index" json:"product_id" validate:"required,uuid"`
	VariantID  *string        `gorm:"index" json:"variant_id,omitempty" validate:"omitempty,uuid"`
	URL        string         `gorm:"not null;size:500" json:"url"`
	Key        string         `gorm:"size:255" json:"key,omitempty"`
	AltText    string         `gorm:"size:255" json:"alt_text,omitempty"`
	Position   int            `gorm:"default:0" json:"position"`
	IsPrimary  bool           `gorm:"default:false" json:"is_primary"`
	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ProductImage) TableName() string {
	return "product_images"
}

type ProductVariant struct {
	ID                string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID        string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	ProductID         string         `gorm:"not null;index" json:"product_id" validate:"required,uuid"`
	Name              string         `gorm:"not null;size:150" json:"name"`
	SKU               string         `gorm:"not null;size:100;index" json:"sku"`
	Barcode           string         `gorm:"size:128;index" json:"barcode,omitempty"`
	Attributes        string         `gorm:"type:jsonb;default:'{}'" json:"attributes,omitempty"`
	IsDefault         bool           `gorm:"default:false;index" json:"is_default"`
	TrackBatches      bool           `gorm:"default:false" json:"track_batches"`
	TrackSerials      bool           `gorm:"default:false" json:"track_serials"`
	Price             float64        `gorm:"type:decimal(15,2);default:0" json:"price"`
	MRP               float64        `gorm:"type:decimal(15,2);default:0" json:"mrp"`
	CostPrice         float64        `gorm:"type:decimal(15,2);default:0" json:"cost_price"`
	DefaultCessRate   float64        `gorm:"type:decimal(7,3);default:0" json:"default_cess_rate"`
	StockLevel        float64        `gorm:"type:decimal(15,3);default:0" json:"stock_level"`
	ReservedLevel     float64        `gorm:"type:decimal(15,3);default:0" json:"reserved_level"`
	LowStockThreshold float64        `gorm:"type:decimal(15,3);default:0" json:"low_stock_threshold"`
	IsActive          bool           `gorm:"default:true;index" json:"is_active"`
	CreatedAt         time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`

	Images []*ProductImage `gorm:"foreignKey:VariantID" json:"images,omitempty"`
}

func (ProductVariant) TableName() string {
	return "product_variants"
}

type ProductCustomColumn struct {
	ID         string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Name       string         `gorm:"not null;size:120" json:"name"`
	Slug       string         `gorm:"not null;size:140" json:"slug"`
	DataType   string         `gorm:"not null;size:40" json:"data_type"`
	Options    string         `gorm:"type:jsonb;default:'[]'" json:"options,omitempty"`
	IsRequired bool           `gorm:"default:false" json:"is_required"`
	SortOrder  int            `gorm:"default:0" json:"sort_order"`
	IsActive   bool           `gorm:"default:true;index" json:"is_active"`
	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ProductCustomColumn) TableName() string {
	return "product_custom_columns"
}

type ProductCustomValue struct {
	ID         string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	ProductID  string         `gorm:"not null;index" json:"product_id" validate:"required,uuid"`
	ColumnID   string         `gorm:"not null;index" json:"column_id" validate:"required,uuid"`
	Value      string         `gorm:"type:jsonb;default:'null'" json:"value,omitempty"`
	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`

	Column *ProductCustomColumn `gorm:"foreignKey:ColumnID" json:"column,omitempty"`
}

func (ProductCustomValue) TableName() string {
	return "product_custom_values"
}

type ProductWarehouseCatalog struct {
	ID            string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID    string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	ProductID     string         `gorm:"not null;index" json:"product_id" validate:"required,uuid"`
	WarehouseID   string         `gorm:"not null;index" json:"warehouse_id" validate:"required,uuid"`
	IsVisible     bool           `gorm:"default:true" json:"is_visible"`
	IsActive      bool           `gorm:"default:true" json:"is_active"`
	PriceOverride *float64       `gorm:"type:decimal(15,2)" json:"price_override,omitempty"`
	PriceListID   *string        `gorm:"index" json:"price_list_id,omitempty" validate:"omitempty,uuid"`
	CreatedAt     time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ProductWarehouseCatalog) TableName() string {
	return "product_warehouse_catalogs"
}

type WarehousePermission struct {
	ID                 string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID         string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	WarehouseID        string         `gorm:"not null;index" json:"warehouse_id" validate:"required,uuid"`
	UserID             string         `gorm:"not null;index" json:"user_id" validate:"required,uuid"`
	CanViewCatalog     bool           `gorm:"default:true" json:"can_view_catalog"`
	CanManageCatalog   bool           `gorm:"default:false" json:"can_manage_catalog"`
	CanMoveStock       bool           `gorm:"default:false" json:"can_move_stock"`
	CanViewReports     bool           `gorm:"default:false" json:"can_view_reports"`
	CanManageWarehouse bool           `gorm:"default:false" json:"can_manage_warehouse"`
	CreatedAt          time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
}

func (WarehousePermission) TableName() string {
	return "warehouse_permissions"
}

type InventoryBalance struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID     string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	ProductID      string         `gorm:"not null;index" json:"product_id" validate:"required,uuid"`
	VariantID      string         `gorm:"not null;index" json:"variant_id" validate:"required,uuid"`
	WarehouseID    string         `gorm:"not null;index" json:"warehouse_id" validate:"required,uuid"`
	BatchID        *string        `gorm:"index" json:"batch_id,omitempty" validate:"omitempty,uuid"`
	BatchKey       string         `gorm:"not null;size:64;default:'';index" json:"batch_key"`
	OnHand         float64        `gorm:"type:decimal(15,3);default:0" json:"on_hand"`
	Reserved       float64        `gorm:"type:decimal(15,3);default:0" json:"reserved"`
	StockValue     float64        `gorm:"type:decimal(15,2);default:0" json:"stock_value"`
	LastRecordedAt *time.Time     `json:"last_recorded_at,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (InventoryBalance) TableName() string {
	return "inventory_balances"
}

type InventorySnapshot struct {
	ID           string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID   string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	SnapshotDate time.Time      `gorm:"not null;index" json:"snapshot_date"`
	ProductID    string         `gorm:"not null;index" json:"product_id" validate:"required,uuid"`
	VariantID    string         `gorm:"not null;index" json:"variant_id" validate:"required,uuid"`
	WarehouseID  string         `gorm:"not null;index" json:"warehouse_id" validate:"required,uuid"`
	OnHand       float64        `gorm:"type:decimal(15,3);default:0" json:"on_hand"`
	Reserved     float64        `gorm:"type:decimal(15,3);default:0" json:"reserved"`
	StockValue   float64        `gorm:"type:decimal(15,2);default:0" json:"stock_value"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (InventorySnapshot) TableName() string {
	return "inventory_snapshots"
}

type ProductBatch struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID     string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	ProductID      string         `gorm:"not null;index" json:"product_id" validate:"required,uuid"`
	VariantID      string         `gorm:"not null;index" json:"variant_id" validate:"required,uuid"`
	BatchNumber    string         `gorm:"not null;size:120;index" json:"batch_number"`
	ManufacturedAt *time.Time     `json:"manufactured_at,omitempty"`
	ExpiresAt      *time.Time     `json:"expires_at,omitempty"`
	ExtraFields    string         `gorm:"type:jsonb;default:'{}'" json:"extra_fields,omitempty"`
	IsActive       bool           `gorm:"default:true;index" json:"is_active"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ProductBatch) TableName() string {
	return "product_batches"
}

type ProductSerialNumber struct {
	ID                 string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID         string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	ProductID          string         `gorm:"not null;index" json:"product_id" validate:"required,uuid"`
	VariantID          string         `gorm:"not null;index" json:"variant_id" validate:"required,uuid"`
	BatchID            *string        `gorm:"index" json:"batch_id,omitempty" validate:"omitempty,uuid"`
	WarehouseID        *string        `gorm:"index" json:"warehouse_id,omitempty" validate:"omitempty,uuid"`
	SerialNumber       string         `gorm:"not null;size:160;index" json:"serial_number"`
	IMEI               string         `gorm:"size:160;index" json:"imei,omitempty"`
	Status             string         `gorm:"not null;size:40;default:'available';index" json:"status"`
	SoldDocumentID     *string        `gorm:"index" json:"sold_document_id,omitempty" validate:"omitempty,uuid"`
	SoldDocumentLineID *string        `gorm:"index" json:"sold_document_line_id,omitempty" validate:"omitempty,uuid"`
	Metadata           string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt          time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ProductSerialNumber) TableName() string {
	return "product_serial_numbers"
}

type AssemblyRecipe struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID     string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Name           string         `gorm:"not null;size:150" json:"name"`
	ProductID      string         `gorm:"not null;index" json:"product_id" validate:"required,uuid"`
	VariantID      string         `gorm:"not null;index" json:"variant_id" validate:"required,uuid"`
	OutputQuantity float64        `gorm:"type:decimal(15,3);default:1" json:"output_quantity"`
	IsActive       bool           `gorm:"default:true;index" json:"is_active"`
	Metadata       string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`

	Components []*AssemblyRecipeComponent `gorm:"foreignKey:RecipeID" json:"components,omitempty"`
}

func (AssemblyRecipe) TableName() string {
	return "assembly_recipes"
}

type AssemblyRecipeComponent struct {
	ID                 string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	RecipeID           string         `gorm:"not null;index" json:"recipe_id" validate:"required,uuid"`
	BusinessID         string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	ComponentProductID string         `gorm:"not null;index" json:"component_product_id" validate:"required,uuid"`
	ComponentVariantID string         `gorm:"not null;index" json:"component_variant_id" validate:"required,uuid"`
	Quantity           float64        `gorm:"type:decimal(15,3);default:1" json:"quantity"`
	CreatedAt          time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
}

func (AssemblyRecipeComponent) TableName() string {
	return "assembly_recipe_components"
}

type InventoryEventLog struct {
	ID         string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	EventType  string         `gorm:"not null;size:80;index" json:"event_type"`
	EntityType string         `gorm:"size:80;index" json:"entity_type,omitempty"`
	EntityID   string         `gorm:"size:80;index" json:"entity_id,omitempty"`
	Payload    string         `gorm:"type:jsonb;default:'{}'" json:"payload,omitempty"`
	CreatedAt  time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

func (InventoryEventLog) TableName() string {
	return "inventory_event_logs"
}
