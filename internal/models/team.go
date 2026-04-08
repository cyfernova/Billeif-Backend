package models

import (
	"time"

	"gorm.io/gorm"
)

type TeamMember struct {
	ID                  string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID          string         `gorm:"not null;index" json:"business_id" validate:"required,uuid"`
	UserID              string         `gorm:"not null;index" json:"user_id" validate:"required,uuid"`
	Role                string         `gorm:"not null;size:50" json:"role" validate:"required,oneof=admin accountant viewer"`
	RoleID              *string        `gorm:"index" json:"role_id,omitempty" validate:"omitempty,uuid"`
	AssignedRole        *Role          `gorm:"foreignKey:RoleID;references:ID" json:"assigned_role,omitempty"`
	Status              string         `gorm:"not null;size:30;default:'active';index" json:"status"`
	BranchScopeJSON     string         `gorm:"type:jsonb;default:'[]'" json:"branch_scope_json,omitempty"`
	PermissionOverrides string         `gorm:"type:jsonb;default:'{}'" json:"permission_overrides,omitempty"`
	InviteEmail         string         `gorm:"size:255" json:"invite_email,omitempty" validate:"omitempty,email,max=255"`
	InviteToken         string         `gorm:"size:255;index" json:"invite_token,omitempty"`
	InvitedBy           *string        `gorm:"index" json:"invited_by,omitempty" validate:"omitempty,uuid"`
	IsOwner             bool           `gorm:"not null;default:false" json:"is_owner"`
	JoinedAt            *time.Time     `json:"joined_at,omitempty"`
	CreatedAt           time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt           time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"index" json:"-"`
}

func (t *TeamMember) TableName() string {
	return "team_members"
}
