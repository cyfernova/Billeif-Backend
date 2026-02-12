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
	log := logger.FromContext(ctx).With("service", "subscription", "operation", "create", "business_id", input.BusinessID, "plan", input.Plan)
	existing, _ := s.repo.GetByBusinessID(ctx, input.BusinessID)
	if existing != nil {
		log.Warn("subscription already exists for business")
		return nil, fmt.Errorf("subscription already exists for this business")
	}

	subscription := &models.Subscription{
		BusinessID: input.BusinessID,
		Plan:       input.Plan,
		Status:     "active",
	}

	if err := s.repo.Create(ctx, subscription); err != nil {
		log.Error("failed to create subscription", "error", err)
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	log.Info("subscription created", "subscription_id", subscription.ID)
	return subscription, nil
}

func (s *SubscriptionService) Get(ctx context.Context, id string) (*models.Subscription, error) {
	log := logger.FromContext(ctx).With("service", "subscription", "operation", "get", "subscription_id", id)
	subscription, err := s.repo.GetByID(ctx, id)
	if err != nil {
		log.Error("failed to get subscription", "error", err)
		return nil, err
	}
	return subscription, nil
}

func (s *SubscriptionService) GetByBusinessID(ctx context.Context, businessID string) (*models.Subscription, error) {
	log := logger.FromContext(ctx).With("service", "subscription", "operation", "get_by_business", "business_id", businessID)
	subscription, err := s.repo.GetByBusinessID(ctx, businessID)
	if err != nil {
		log.Error("failed to get subscription by business", "error", err)
		return nil, err
	}
	return subscription, nil
}

type UpdateSubscriptionInput struct {
	Plan   string `json:"plan,omitempty"`
	Status string `json:"status,omitempty"`
}

func (s *SubscriptionService) Update(ctx context.Context, businessID string, input UpdateSubscriptionInput) (*models.Subscription, error) {
	log := logger.FromContext(ctx).With("service", "subscription", "operation", "update", "business_id", businessID)
	subscription, err := s.repo.GetByBusinessID(ctx, businessID)
	if err != nil {
		log.Error("failed to load subscription for update", "error", err)
		return nil, err
	}

	if input.Plan != "" {
		subscription.Plan = input.Plan
	}
	if input.Status != "" {
		subscription.Status = input.Status
	}

	if err := s.repo.Update(ctx, subscription); err != nil {
		log.Error("failed to update subscription", "error", err)
		return nil, err
	}

	log.Info("subscription updated", "subscription_id", subscription.ID, "plan", subscription.Plan, "status", subscription.Status)
	return subscription, nil
}
