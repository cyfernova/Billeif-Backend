package services

import (
	"context"
	"fmt"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type SubscriptionService struct {
	repo interfaces.SubscriptionRepository
	log  *logger.Logger
}

func NewSubscriptionService(repo interfaces.SubscriptionRepository, log *logger.Logger) *SubscriptionService {
	return &SubscriptionService{repo: repo, log: log}
}

type CreateSubscriptionInput struct {
	BusinessID string `json:"business_id" binding:"required,uuid"`
	Plan       string `json:"plan" binding:"required,oneof=free starter professional enterprise"`
}

func (s *SubscriptionService) Create(ctx context.Context, input CreateSubscriptionInput) (*models.Subscription, error) {
	existing, _ := s.repo.GetByBusinessID(ctx, input.BusinessID)
	if existing != nil {
		return nil, fmt.Errorf("subscription already exists for this business")
	}

	subscription := &models.Subscription{
		BusinessID: input.BusinessID,
		Plan:       input.Plan,
		Status:     "active",
	}

	if err := s.repo.Create(ctx, subscription); err != nil {
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	return subscription, nil
}

func (s *SubscriptionService) Get(ctx context.Context, id string) (*models.Subscription, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *SubscriptionService) GetByBusinessID(ctx context.Context, businessID string) (*models.Subscription, error) {
	return s.repo.GetByBusinessID(ctx, businessID)
}

type UpdateSubscriptionInput struct {
	Plan   string `json:"plan,omitempty"`
	Status string `json:"status,omitempty"`
}

func (s *SubscriptionService) Update(ctx context.Context, businessID string, input UpdateSubscriptionInput) (*models.Subscription, error) {
	subscription, err := s.repo.GetByBusinessID(ctx, businessID)
	if err != nil {
		return nil, err
	}

	if input.Plan != "" {
		subscription.Plan = input.Plan
	}
	if input.Status != "" {
		subscription.Status = input.Status
	}

	if err := s.repo.Update(ctx, subscription); err != nil {
		return nil, err
	}

	return subscription, nil
}
