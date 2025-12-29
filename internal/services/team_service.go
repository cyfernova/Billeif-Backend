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
	BusinessID string `json:"business_id" binding:"required,uuid"`
	UserID     string `json:"user_id" binding:"required,uuid"`
	Role       string `json:"role" binding:"required,oneof=admin accountant viewer"`
}

func (s *TeamService) Create(ctx context.Context, input CreateTeamMemberInput) (*models.TeamMember, error) {
	member := &models.TeamMember{
		BusinessID: input.BusinessID,
		UserID:     input.UserID,
		Role:       input.Role,
	}

	if err := s.repo.Create(ctx, member); err != nil {
		return nil, fmt.Errorf("failed to create team member: %w", err)
	}

	return member, nil
}

func (s *TeamService) Get(ctx context.Context, id string) (*models.TeamMember, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *TeamService) List(ctx context.Context, businessID string, page, limit int) ([]*models.TeamMember, int64, error) {
	return s.repo.GetByBusinessID(ctx, businessID, page, limit)
}

type UpdateTeamMemberInput struct {
	Role string `json:"role" binding:"required,oneof=admin accountant viewer"`
}

func (s *TeamService) Update(ctx context.Context, id string, input UpdateTeamMemberInput) (*models.TeamMember, error) {
	member, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	member.Role = input.Role

	if err := s.repo.Update(ctx, member); err != nil {
		return nil, err
	}

	return member, nil
}

func (s *TeamService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}
