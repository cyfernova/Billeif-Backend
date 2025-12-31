package models

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID                string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	Email             string         `gorm:"uniqueIndex;not null;size:255" json:"email" validate:"required,email"`
	CognitoID         string         `gorm:"uniqueIndex;not null;size:255" json:"cognito_id" validate:"required"`
	Name              string         `gorm:"not null;size:255" json:"name" validate:"required,min=2,max=255"`
	ProfilePictureURL string         `gorm:"size:2048" json:"profile_picture_url,omitempty"`
	Role              string         `gorm:"not null;default:'viewer';size:50" json:"role" validate:"required,oneof=admin accountant viewer"`
	BusinessID        *string        `gorm:"type:uuid;index" json:"business_id,omitempty" validate:"omitempty,uuid"`
	CreatedAt         time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

func (u *User) TableName() string {
	return "users"
}
