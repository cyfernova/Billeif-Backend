package services

import (
	"context"
	"fmt"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type TeamService struct {
	repo interfaces.TeamMemberRepository
	log  *logger.Logger
}

func NewTeamService(repo interfaces.TeamMemberRepository, log *logger.Logger) *TeamService {
	return &TeamService{repo: repo, log: log}
}

type CreateTeamMemberInput struct {
	BusinessID string `json:"business_id,omitempty"`
	UserID     string `json:"user_id" binding:"required,uuid"`
	Role       string `json:"role" binding:"required,oneof=admin accountant viewer"`
}

func (s *TeamService) Create(ctx context.Context, input CreateTeamMemberInput) (*models.TeamMember, error) {
	log := logger.FromContext(ctx).With("service", "team", "operation", "create", "business_id", input.BusinessID, "user_id", input.UserID)
	member := &models.TeamMember{
		BusinessID: input.BusinessID,
		UserID:     input.UserID,
		Role:       input.Role,
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
	Role string `json:"role" binding:"required,oneof=admin accountant viewer"`
}

func (s *TeamService) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdateTeamMemberInput) (*models.TeamMember, error) {
	log := logger.FromContext(ctx).With("service", "team", "operation", "update", "team_member_id", id, "business_id", businessID)
	member, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		log.Error("failed to load team member for scoped update", "error", err)
		return nil, err
	}

	member.Role = input.Role
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
