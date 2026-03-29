package models

import (
	"time"

	"gorm.io/gorm"
)

type Project struct {
	ID          string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID  string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	Name        string         `gorm:"not null;size:150" json:"name"`
	Code        string         `gorm:"not null;size:80" json:"code"`
	Description string         `gorm:"type:text" json:"description,omitempty"`
	Color       string         `gorm:"size:32" json:"color,omitempty"`
	IsActive    bool           `gorm:"default:true;index" json:"is_active"`
	Metadata    string         `gorm:"type:jsonb;default:'{}'" json:"metadata,omitempty"`
	CreatedAt   time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Project) TableName() string {
	return "projects"
}
