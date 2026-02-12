package models

import (
	"time"

	"gorm.io/gorm"
)

type Webhook struct {
	ID            string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID    string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Name          string         `gorm:"not null;size:255" json:"name" validate:"required,min=2,max=255"`
	URL           string         `gorm:"not null;size:500" json:"url" validate:"required,url"`
	Events        string         `gorm:"not null;type:text" json:"events" validate:"required"`
	Secret        string         `gorm:"not null;size:255" json:"-" validate:"required"`
	IsActive      bool           `gorm:"default:true" json:"is_active"`
	LastTriggered *time.Time     `json:"last_triggered,omitempty"`
	CreatedAt     time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

func (w *Webhook) TableName() string {
	return "webhooks"
}
