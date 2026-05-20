package models

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

type LLMChatConversation struct {
	ID           string         `gorm:"primaryKey;type:uuid" json:"id"`
	BusinessID   string         `gorm:"type:uuid;not null;index" json:"business_id"`
	UserID       string         `gorm:"not null;size:255;index" json:"user_id"`
	Title        string         `gorm:"not null;size:160" json:"title"`
	LastMessage  string         `gorm:"type:text;not null;default:''" json:"last_message"`
	MessageCount int            `gorm:"not null;default:0" json:"message_count"`
	CreatedAt    time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (LLMChatConversation) TableName() string {
	return "llm_chat_conversations"
}

type LLMChatMessageRecord struct {
	ID             string          `gorm:"primaryKey;type:uuid" json:"id"`
	ConversationID string          `gorm:"type:uuid;not null;index" json:"conversation_id"`
	BusinessID     string          `gorm:"type:uuid;not null;index" json:"business_id"`
	UserID         string          `gorm:"not null;size:255;index" json:"user_id"`
	Role           string          `gorm:"not null;size:20" json:"role"`
	Content        string          `gorm:"type:text;not null" json:"content"`
	WebSearchRaw   json.RawMessage `gorm:"column:web_search;type:jsonb" json:"-"`
	CreatedAt      time.Time       `gorm:"autoCreateTime" json:"created_at"`
}

func (LLMChatMessageRecord) TableName() string {
	return "llm_chat_messages"
}
