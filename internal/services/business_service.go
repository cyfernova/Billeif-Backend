package services

import (
	"context"
	"errors"
	"fmt"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type BusinessService struct {
	repo interfaces.BusinessRepository
	s3   *S3Service
	log  *logger.Logger
}

func NewBusinessService(repo interfaces.BusinessRepository, s3 *S3Service, log *logger.Logger) *BusinessService {
	return &BusinessService{repo: repo, s3: s3, log: log}
}

type CreateBusinessInput struct {
	Name          string `json:"name" binding:"required,min=2"`
	Email         string `json:"email" binding:"required,email"`
	Phone         string `json:"phone"`
	Address       string `json:"address"`
	City          string `json:"city"`
	State         string `json:"state"`
	Country       string `json:"country"`
	ZipCode       string `json:"zip_code"`
	TaxID         string `json:"tax_id"`
	Currency      string `json:"currency"`
	InvoicePrefix string `json:"invoice_prefix"`
}

func (s *BusinessService) Create(ctx context.Context, userID string, input CreateBusinessInput) (*models.BusinessProfile, error) {
	log := logger.FromContext(ctx).With("service", "business", "operation", "create", "owner_id", userID)
	business := &models.BusinessProfile{
		OwnerID:    userID,
		Name:       input.Name,
		Email:      input.Email,
		Phone:      input.Phone,
		Address:    input.Address,
		City:       input.City,
		State:      input.State,
		Country:    input.Country,
		PostalCode: input.ZipCode,
		TaxID:      input.TaxID,
		Currency:   input.Currency,
	}

	if business.Currency == "" {
		business.Currency = "USD"
	}

	if err := s.repo.Create(ctx, business); err != nil {
		log.Error("failed to create business", "error", err)
		return nil, fmt.Errorf("failed to create business: %w", err)
	}

	log.Info("business created", "business_id", business.ID)
	return business, nil
}

func (s *BusinessService) Get(ctx context.Context, id string) (*models.BusinessProfile, error) {
	log := logger.FromContext(ctx).With("service", "business", "operation", "get", "business_id", id)
	business, err := s.repo.GetByID(ctx, id)
	if err != nil {
		log.Error("failed to get business", "error", err)
		return nil, err
	}
	return business, nil
}

func (s *BusinessService) GetByOwner(ctx context.Context, userID, businessID string) (*models.BusinessProfile, error) {
	business, err := s.Get(ctx, businessID)
	if err != nil {
		return nil, err
	}
	if business.OwnerID != userID {
		return nil, errors.New("business not found")
	}
	return business, nil
}

func (s *BusinessService) List(ctx context.Context, userID string, page, limit int) ([]*models.BusinessProfile, int64, error) {
	log := logger.FromContext(ctx).With("service", "business", "operation", "list", "owner_id", userID, "page", page, "limit", limit)
	businesses, total, err := s.repo.List(ctx, userID, page, limit)
	if err != nil {
		log.Error("failed to list businesses", "error", err)
		return nil, 0, err
	}
	log.Debug("listed businesses", "count", len(businesses), "total", total)
	return businesses, total, nil
}

type UpdateBusinessInput struct {
	Name          string `json:"name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	Address       string `json:"address"`
	City          string `json:"city"`
	State         string `json:"state"`
	Country       string `json:"country"`
	ZipCode       string `json:"zip_code"`
	TaxID         string `json:"tax_id"`
	Currency      string `json:"currency"`
	InvoicePrefix string `json:"invoice_prefix"`
}

func (s *BusinessService) Update(ctx context.Context, id string, input UpdateBusinessInput) (*models.BusinessProfile, error) {
	log := logger.FromContext(ctx).With("service", "business", "operation", "update", "business_id", id)
	business, err := s.repo.GetByID(ctx, id)
	if err != nil {
		log.Error("failed to load business for update", "error", err)
		return nil, err
	}

	if input.Name != "" {
		business.Name = input.Name
	}
	if input.Email != "" {
		business.Email = input.Email
	}
	if input.Phone != "" {
		business.Phone = input.Phone
	}
	if input.Address != "" {
		business.Address = input.Address
	}
	if input.City != "" {
		business.City = input.City
	}
	if input.State != "" {
		business.State = input.State
	}
	if input.Country != "" {
		business.Country = input.Country
	}
	if input.ZipCode != "" {
		business.PostalCode = input.ZipCode
	}
	if input.TaxID != "" {
		business.TaxID = input.TaxID
	}
	if input.Currency != "" {
		business.Currency = input.Currency
	}

	if err := s.repo.Update(ctx, business); err != nil {
		log.Error("failed to update business", "error", err)
		return nil, err
	}

	log.Info("business updated", "business_id", business.ID)
	return business, nil
}

func (s *BusinessService) UpdateByOwner(ctx context.Context, userID, id string, input UpdateBusinessInput) (*models.BusinessProfile, error) {
	log := logger.FromContext(ctx).With("service", "business", "operation", "update", "business_id", id, "owner_id", userID)
	business, err := s.GetByOwner(ctx, userID, id)
	if err != nil {
		log.Error("failed to load business for scoped update", "error", err)
		return nil, err
	}

	if input.Name != "" {
		business.Name = input.Name
	}
	if input.Email != "" {
		business.Email = input.Email
	}
	if input.Phone != "" {
		business.Phone = input.Phone
	}
	if input.Address != "" {
		business.Address = input.Address
	}
	if input.City != "" {
		business.City = input.City
	}
	if input.State != "" {
		business.State = input.State
	}
	if input.Country != "" {
		business.Country = input.Country
	}
	if input.ZipCode != "" {
		business.PostalCode = input.ZipCode
	}
	if input.TaxID != "" {
		business.TaxID = input.TaxID
	}
	if input.Currency != "" {
		business.Currency = input.Currency
	}

	if err := s.repo.Update(ctx, business); err != nil {
		log.Error("failed to update business", "error", err)
		return nil, err
	}
	log.Info("business updated", "business_id", business.ID)
	return business, nil
}

func (s *BusinessService) Delete(ctx context.Context, id string) error {
	log := logger.FromContext(ctx).With("service", "business", "operation", "delete", "business_id", id)
	if err := s.repo.Delete(ctx, id); err != nil {
		log.Error("failed to delete business", "error", err)
		return err
	}
	log.Info("business deleted", "business_id", id)
	return nil
}

func (s *BusinessService) DeleteByOwner(ctx context.Context, userID, id string) error {
	log := logger.FromContext(ctx).With("service", "business", "operation", "delete", "business_id", id, "owner_id", userID)
	business, err := s.GetByOwner(ctx, userID, id)
	if err != nil {
		log.Error("failed to load business for scoped delete", "error", err)
		return err
	}
	if err := s.repo.Delete(ctx, business.ID); err != nil {
		log.Error("failed to delete business", "error", err)
		return err
	}
	log.Info("business deleted", "business_id", business.ID)
	return nil
}

func (s *BusinessService) GetLogoUploadURL(ctx context.Context, businessID, contentType string) (string, error) {
	log := logger.FromContext(ctx).With("service", "business", "operation", "get_logo_upload_url", "business_id", businessID)
	key := fmt.Sprintf("logos/%s/logo", businessID)
	url, err := s.s3.GeneratePresignedUploadURL(ctx, "business-logos", key, contentType, 3600)
	if err != nil {
		log.Error("failed to generate business logo upload URL", "error", err)
		return "", err
	}
	log.Debug("generated business logo upload URL")
	return url, nil
}

func (s *BusinessService) GetLogoUploadURLByOwner(ctx context.Context, userID, businessID, contentType string) (string, error) {
	if _, err := s.GetByOwner(ctx, userID, businessID); err != nil {
		return "", err
	}
	return s.GetLogoUploadURL(ctx, businessID, contentType)
}

func (s *BusinessService) UpdateLogoURL(ctx context.Context, businessID, logoURL string) error {
	log := logger.FromContext(ctx).With("service", "business", "operation", "update_logo_url", "business_id", businessID)
	business, err := s.repo.GetByID(ctx, businessID)
	if err != nil {
		log.Error("failed to load business for logo update", "error", err)
		return err
	}
	business.LogoURL = logoURL
	if err := s.repo.Update(ctx, business); err != nil {
		log.Error("failed to update business logo URL", "error", err)
		return err
	}
	log.Info("business logo URL updated")
	return nil
}
