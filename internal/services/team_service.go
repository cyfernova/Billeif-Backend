package services

import (
	"context"
	"fmt"
	"strings"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"gorm.io/gorm"
)

type TeamService struct {
	repo interfaces.TeamMemberRepository
	log  *logger.Logger
	db   *gorm.DB
}

func NewTeamService(repo interfaces.TeamMemberRepository, log *logger.Logger) *TeamService {
	return &TeamService{repo: repo, log: log}
}

func (s *TeamService) WithDB(db *gorm.DB) *TeamService {
	s.db = db
	return s
}

type CreateTeamMemberInput struct {
	BusinessID string `json:"business_id,omitempty"`
	UserID     string `json:"user_id" binding:"required,uuid"`
	Role       string `json:"role,omitempty"`
	RoleID     string `json:"role_id,omitempty" binding:"omitempty,uuid"`
}

func (s *TeamService) Create(ctx context.Context, input CreateTeamMemberInput) (*models.TeamMember, error) {
	log := logger.FromContext(ctx).With("service", "team", "operation", "create", "business_id", input.BusinessID, "user_id", input.UserID)
	role, roleID, assignedRole, err := s.resolveRoleAssignment(ctx, input.BusinessID, input.Role, input.RoleID)
	if err != nil {
		return nil, err
	}
	member := &models.TeamMember{
		BusinessID:   input.BusinessID,
		UserID:       input.UserID,
		Role:         role,
		RoleID:       roleID,
		AssignedRole: assignedRole,
	}

	if err := s.repo.Create(ctx, member); err != nil {
		log.Error("failed to create team member", "error", err)
		return nil, fmt.Errorf("failed to create team member: %w", err)
	}

	log.Info("team member created", "team_member_id", member.ID, "role", member.Role)
	return member, nil
}

func (s *TeamService) GetByBusiness(ctx context.Context, businessID, id string) (*models.TeamMember, error) {
	return s.repo.GetByID(ctx, id, businessID)
}

func (s *TeamService) List(ctx context.Context, businessID string, page, limit int) ([]*models.TeamMember, int64, error) {
	log := logger.FromContext(ctx).With("service", "team", "operation", "list", "business_id", businessID, "page", page, "limit", limit)
	members, total, err := s.repo.GetByBusinessID(ctx, businessID, page, limit)
	if err != nil {
		log.Error("failed to list team members", "error", err)
		return nil, 0, err
	}
	log.Debug("listed team members", "count", len(members), "total", total)
	return members, total, nil
}

type UpdateTeamMemberInput struct {
	Role   string `json:"role,omitempty"`
	RoleID string `json:"role_id,omitempty" binding:"omitempty,uuid"`
}

func (s *TeamService) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdateTeamMemberInput) (*models.TeamMember, error) {
	log := logger.FromContext(ctx).With("service", "team", "operation", "update", "team_member_id", id, "business_id", businessID)
	member, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		log.Error("failed to load team member for scoped update", "error", err)
		return nil, err
	}

	if input.Role != "" || input.RoleID != "" {
		role, roleID, assignedRole, err := s.resolveRoleAssignment(ctx, businessID, input.Role, input.RoleID)
		if err != nil {
			return nil, err
		}
		member.Role = role
		member.RoleID = roleID
		member.AssignedRole = assignedRole
	} else if input.Role != "" {
		member.RoleID = nil
		member.AssignedRole = nil
	}
	if err := s.repo.Update(ctx, member); err != nil {
		log.Error("failed to update team member", "error", err)
		return nil, err
	}
	log.Info("team member updated", "team_member_id", member.ID, "role", member.Role)
	return member, nil
}

func (s *TeamService) Delete(ctx context.Context, id string) error {
	log := logger.FromContext(ctx).With("service", "team", "operation", "delete", "team_member_id", id)
	if err := s.repo.Delete(ctx, id); err != nil {
		log.Error("failed to delete team member", "error", err)
		return err
	}
	log.Info("team member deleted", "team_member_id", id)
	return nil
}

func (s *TeamService) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	log := logger.FromContext(ctx).With("service", "team", "operation", "delete", "team_member_id", id, "business_id", businessID)
	member, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		log.Error("failed to load team member for scoped delete", "error", err)
		return err
	}
	if err := s.repo.Delete(ctx, member.ID); err != nil {
		log.Error("failed to delete team member", "error", err)
		return err
	}
	log.Info("team member deleted", "team_member_id", member.ID)
	return nil
}

func (s *TeamService) resolveRoleAssignment(ctx context.Context, businessID, rawRole, rawRoleID string) (string, *string, *models.Role, error) {
	role := normalizeTeamRole(rawRole)
	roleID := strings.TrimSpace(rawRoleID)

	if role == "" && roleID == "" {
		return "", nil, nil, fmt.Errorf("either role or role_id is required")
	}
	if role != "" && !isSupportedTeamRole(role) {
		return "", nil, nil, fmt.Errorf("unsupported team role")
	}
	if roleID == "" {
		return role, nil, nil, nil
	}
	if s.db == nil {
		return "", nil, nil, fmt.Errorf("database is not configured")
	}

	var assignedRole models.Role
	if err := s.db.WithContext(ctx).
		Preload("Permissions", "deleted_at IS NULL").
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", roleID, businessID).
		First(&assignedRole).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", nil, nil, fmt.Errorf("role not found")
		}
		return "", nil, nil, err
	}

	if role == "" {
		role = "viewer"
		if assignedRole.IsSystem && isSupportedTeamRole(assignedRole.Key) {
			role = assignedRole.Key
		}
	}

	return role, &assignedRole.ID, &assignedRole, nil
}

func normalizeTeamRole(role string) string {
	return strings.ToLower(strings.TrimSpace(role))
}

func isSupportedTeamRole(role string) bool {
	switch role {
	case "admin", "accountant", "viewer":
		return true
	default:
		return false
	}
}
