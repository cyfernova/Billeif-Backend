package models

import (
	"time"

	"github.com/google/uuid"
)

// UserRole represents user roles
type UserRole string

const (
	RoleAdmin      UserRole = "admin"
	RoleAccountant UserRole = "accountant"
	RoleViewer     UserRole = "viewer"
	RoleUser       UserRole = "user"
)

// User represents a user in the system
type User struct {
	ID               uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	CognitoID        string     `gorm:"column:cognito_id;uniqueIndex;size:255;not null" json:"cognito_id"`
	Email            string     `gorm:"uniqueIndex;size:255;not null" json:"email"`
	FirstName        string     `gorm:"size:100" json:"first_name"`
	LastName         string     `gorm:"size:100" json:"last_name"`
	Phone            string     `gorm:"size:20" json:"phone,omitempty"`
	ProfilePictureURL string    `gorm:"column:profile_picture_url;size:500" json:"profile_picture_url,omitempty"`
	Role             UserRole   `gorm:"size:50;not null;default:'user'" json:"role"`
	IsActive         bool       `gorm:"column:is_active;default:true" json:"is_active"`
	EmailVerified    bool       `gorm:"column:email_verified;default:false" json:"email_verified"`
	CreatedAt        time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt        *time.Time `gorm:"column:deleted_at;index" json:"deleted_at,omitempty"`
}

// TableName returns the table name for User
func (User) TableName() string {
	return "users"
}

// UserPreference represents user preferences stored in DynamoDB
type UserPreference struct {
	UserID       string            `dynamodbav:"user_id" json:"user_id"`
	Theme        string            `dynamodbav:"theme" json:"theme"`
	Language     string            `dynamodbav:"language" json:"language"`
	Currency     string            `dynamodbav:"currency" json:"currency"`
	DateFormat   string            `dynamodbav:"date_format" json:"date_format"`
	Timezone     string            `dynamodbav:"timezone" json:"timezone"`
	Notifications map[string]bool  `dynamodbav:"notifications" json:"notifications"`
	UpdatedAt    time.Time         `dynamodbav:"updated_at" json:"updated_at"`
}

// IsValidRole checks if a role is valid
func IsValidRole(role UserRole) bool {
	switch role {
	case RoleAdmin, RoleAccountant, RoleViewer, RoleUser:
		return true
	default:
		return false
	}
}

// GetPermissions returns permissions for a role
func (r UserRole) GetPermissions() []string {
	switch r {
	case RoleAdmin:
		return []string{"read", "write", "delete", "manage_users", "manage_settings"}
	case RoleAccountant:
		return []string{"read", "write", "delete"}
	case RoleViewer:
		return []string{"read"}
	default:
		return []string{"read"}
	}
}

// HasPermission checks if role has a specific permission
func (r UserRole) HasPermission(permission string) bool {
	for _, p := range r.GetPermissions() {
		if p == permission {
			return true
		}
	}
	return false
}
