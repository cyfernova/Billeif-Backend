package models

import (
	"time"

	"gorm.io/gorm"
)

type Product struct {
	ID          string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID  string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Name        string         `gorm:"not null;size:255" json:"name" validate:"required,min=2,max=255"`
	SKU         string         `gorm:"not null;uniqueIndex:idx_business_sku;size:100" json:"sku" validate:"required,max=100"`
	Description string         `gorm:"type:text" json:"description,omitempty"`
	Price       float64        `gorm:"not null;type:decimal(15,2)" json:"price" validate:"required,gt=0"`
	Currency    string         `gorm:"not null;size:3;default:'USD'" json:"currency" validate:"required,len=3"`
	Unit        string         `gorm:"not null;size:50;default:'PCS'" json:"unit" validate:"required,max=50"`
	StockLevel  int64          `gorm:"default:0" json:"stock_level" validate:"gte=0"`
	MinStock    int64          `gorm:"default:0" json:"min_stock" validate:"gte=0"`
	ImageURL    string         `gorm:"size:500" json:"image_url,omitempty"`
	ImageKey    string         `gorm:"size:255" json:"image_key,omitempty"`
	IsActive    bool           `gorm:"default:true" json:"is_active"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (p *Product) TableName() string {
	return "products"
}
