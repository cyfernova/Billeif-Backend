package models

import (
	"time"
)

// Session represents a user session stored in DynamoDB
type Session struct {
	SessionID    string    `dynamodbav:"session_id" json:"session_id"`
	UserID       string    `dynamodbav:"user_id" json:"user_id"`
	RefreshToken string    `dynamodbav:"refresh_token" json:"-"`
	IPAddress    string    `dynamodbav:"ip_address" json:"ip_address"`
	UserAgent    string    `dynamodbav:"user_agent" json:"user_agent"`
	ExpiresAt    time.Time `dynamodbav:"expires_at" json:"expires_at"`
	CreatedAt    time.Time `dynamodbav:"created_at" json:"created_at"`
}

// IsExpired checks if the session is expired
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}
