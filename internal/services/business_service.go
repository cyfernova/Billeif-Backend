package services

import (
	"context"
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
		return nil, fmt.Errorf("failed to create business: %w", err)
	}

	return business, nil
}

func (s *BusinessService) Get(ctx context.Context, id string) (*models.BusinessProfile, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *BusinessService) List(ctx context.Context, userID string, page, limit int) ([]*models.BusinessProfile, int64, error) {
	return s.repo.List(ctx, userID, page, limit)
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
	business, err := s.repo.GetByID(ctx, id)
	if err != nil {
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
		return nil, err
	}

	return business, nil
}

func (s *BusinessService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

func (s *BusinessService) GetLogoUploadURL(ctx context.Context, businessID, contentType string) (string, error) {
	key := fmt.Sprintf("logos/%s/logo", businessID)
	return s.s3.GeneratePresignedUploadURL(ctx, "business-logos", key, contentType, 3600)
}

func (s *BusinessService) UpdateLogoURL(ctx context.Context, businessID, logoURL string) error {
	business, err := s.repo.GetByID(ctx, businessID)
	if err != nil {
		return err
	}
	business.LogoURL = logoURL
	return s.repo.Update(ctx, business)
}
