package models

import "time"

type Notification struct {
	ID             string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID     string     `gorm:"column:business_id;type:uuid;not null;index" json:"business_id"`
	UserID         string     `gorm:"column:user_id;size:255;not null;index" json:"user_id"`
	SourceEventKey string     `gorm:"column:source_event_key;size:255;not null" json:"-"`
	Type           string     `gorm:"column:type;size:32;not null" json:"type"`
	Title          string     `gorm:"column:title;size:255;not null" json:"title"`
	Body           string     `gorm:"column:body;type:text;not null" json:"body"`
	ResourceType   string     `gorm:"column:resource_type;size:64" json:"resource_type,omitempty"`
	ResourceID     string     `gorm:"column:resource_id;size:255" json:"resource_id,omitempty"`
	ReadAt         *time.Time `gorm:"column:read_at;index" json:"read_at,omitempty"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"-"`
}

func (Notification) TableName() string {
	return "notifications"
}
