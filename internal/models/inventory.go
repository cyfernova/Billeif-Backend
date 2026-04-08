package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	StockMoveDirectionIn      = "in"
	StockMoveDirectionOut     = "out"
	StockMoveDirectionReserve = "reserve"
	StockMoveDirectionRelease = "release"
)

type Warehouse struct {
	ID                string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID        string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	BranchID          *string        `gorm:"index" json:"branch_id,omitempty" validate:"omitempty,uuid"`
	Name              string         `gorm:"not null;size:120" json:"name"`
	Code              string         `gorm:"not null;size:60" json:"code"`
	Address           string         `gorm:"size:500" json:"address,omitempty"`
	City              string         `gorm:"size:100" json:"city,omitempty"`
	State             string         `gorm:"size:100" json:"state,omitempty"`
	Country           string         `gorm:"size:100" json:"country,omitempty"`
	PostalCode        string         `gorm:"size:20" json:"postal_code,omitempty"`
	IsDefault         bool           `gorm:"default:false" json:"is_default"`
	CreatedAt         time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
	ProductCount      int64          `gorm:"-" json:"product_count,omitempty"`
	LowStockCount     int64          `gorm:"-" json:"low_stock_count,omitempty"`
	PermissionSummary []string       `gorm:"-" json:"permission_summary,omitempty"`
}

func (Warehouse) TableName() string {
	return "warehouses"
}

type StockMove struct {
	ID                string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID        string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	ProductID         string         `gorm:"not null;index" json:"product_id" validate:"required,uuid"`
	VariantID         *string        `gorm:"index" json:"variant_id,omitempty" validate:"omitempty,uuid"`
	WarehouseID       *string        `gorm:"index" json:"warehouse_id,omitempty" validate:"omitempty,uuid"`
	SourceWarehouseID *string        `gorm:"index" json:"source_warehouse_id,omitempty" validate:"omitempty,uuid"`
	DocumentID        *string        `gorm:"index" json:"document_id,omitempty" validate:"omitempty,uuid"`
	DocumentLineID    *string        `gorm:"index" json:"document_line_id,omitempty" validate:"omitempty,uuid"`
	ProjectID         *string        `gorm:"index" json:"project_id,omitempty" validate:"omitempty,uuid"`
	BatchID           *string        `gorm:"index" json:"batch_id,omitempty" validate:"omitempty,uuid"`
	SerialNumberID    *string        `gorm:"index" json:"serial_number_id,omitempty" validate:"omitempty,uuid"`
	TransactionType   string         `gorm:"size:40;index" json:"transaction_type,omitempty"`
	Direction         string         `gorm:"not null;size:20;index" json:"direction"`
	Quantity          float64        `gorm:"type:decimal(15,3);not null" json:"quantity"`
	UnitCost          float64        `gorm:"type:decimal(15,2);default:0" json:"unit_cost"`
	Reason            string         `gorm:"size:255" json:"reason,omitempty"`
	ActorID           *string        `gorm:"index" json:"actor_id,omitempty" validate:"omitempty,uuid"`
	ActorRole         string         `gorm:"size:80" json:"actor_role,omitempty"`
	Metadata          string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	RecordedAt        time.Time      `gorm:"not null;index" json:"recorded_at"`
	CreatedAt         time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

func (StockMove) TableName() string {
	return "stock_moves"
}

type InventoryReservation struct {
	ID             string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID     string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	ProductID      string         `gorm:"not null;index" json:"product_id" validate:"required,uuid"`
	WarehouseID    *string        `gorm:"index" json:"warehouse_id,omitempty" validate:"omitempty,uuid"`
	DocumentID     string         `gorm:"not null;index" json:"document_id" validate:"required,uuid"`
	DocumentLineID *string        `gorm:"index" json:"document_line_id,omitempty" validate:"omitempty,uuid"`
	Quantity       float64        `gorm:"type:decimal(15,3);not null" json:"quantity"`
	Status         string         `gorm:"not null;size:30;default:'active';index" json:"status"`
	ExpiresAt      *time.Time     `json:"expires_at,omitempty"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (InventoryReservation) TableName() string {
	return "inventory_reservations"
}
