package models

import "time"

type WebSocketTicket struct {
	ID           string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"-"`
	TicketDigest string     `gorm:"column:ticket_digest;type:char(64);not null;uniqueIndex" json:"-"`
	Subject      string     `gorm:"column:subject;size:255;not null;index" json:"-"`
	BusinessID   string     `gorm:"column:business_id;type:uuid;not null;index" json:"-"`
	ExpiresAt    time.Time  `gorm:"column:expires_at;not null;index" json:"expires_at"`
	ConsumedAt   *time.Time `gorm:"column:consumed_at;index" json:"-"`
	CreatedAt    time.Time  `gorm:"autoCreateTime" json:"-"`
	UpdatedAt    time.Time  `gorm:"autoUpdateTime" json:"-"`
}

func (WebSocketTicket) TableName() string {
	return "websocket_tickets"
}
