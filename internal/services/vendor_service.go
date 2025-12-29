package services

import (
	"context"
	"fmt"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type VendorService struct {
	repo interfaces.VendorRepository
	log  *logger.Logger
}

func NewVendorService(repo interfaces.VendorRepository, log *logger.Logger) *VendorService {
	return &VendorService{repo: repo, log: log}
}

type CreateVendorInput struct {
	BusinessID   string `json:"business_id" binding:"required,uuid"`
	Name         string `json:"name" binding:"required,min=2"`
	Email        string `json:"email" binding:"required,email"`
	Phone        string `json:"phone"`
	Address      string `json:"address"`
	City         string `json:"city"`
	State        string `json:"state"`
	Country      string `json:"country"`
	PostalCode   string `json:"postal_code"`
	TaxID        string `json:"tax_id"`
	PaymentTerms string `json:"payment_terms"`
}

func (s *VendorService) Create(ctx context.Context, input CreateVendorInput) (*models.Vendor, error) {
	vendor := &models.Vendor{
		BusinessID:   input.BusinessID,
		Name:         input.Name,
		Email:        input.Email,
		Phone:        input.Phone,
		Address:      input.Address,
		City:         input.City,
		State:        input.State,
		Country:      input.Country,
		PostalCode:   input.PostalCode,
		TaxID:        input.TaxID,
		PaymentTerms: input.PaymentTerms,
	}

	if err := s.repo.Create(ctx, vendor); err != nil {
		return nil, fmt.Errorf("failed to create vendor: %w", err)
	}

	return vendor, nil
}

func (s *VendorService) Get(ctx context.Context, id string) (*models.Vendor, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *VendorService) List(ctx context.Context, businessID string, page, limit int) ([]*models.Vendor, int64, error) {
	return s.repo.GetByBusinessID(ctx, businessID, page, limit)
}

type UpdateVendorInput struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	Phone        string `json:"phone"`
	Address      string `json:"address"`
	City         string `json:"city"`
	State        string `json:"state"`
	Country      string `json:"country"`
	PostalCode   string `json:"postal_code"`
	TaxID        string `json:"tax_id"`
	PaymentTerms string `json:"payment_terms"`
}

func (s *VendorService) Update(ctx context.Context, id string, input UpdateVendorInput) (*models.Vendor, error) {
	vendor, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.Name != "" {
		vendor.Name = input.Name
	}
	if input.Email != "" {
		vendor.Email = input.Email
	}
	if input.Phone != "" {
		vendor.Phone = input.Phone
	}
	if input.Address != "" {
		vendor.Address = input.Address
	}
	if input.City != "" {
		vendor.City = input.City
	}
	if input.State != "" {
		vendor.State = input.State
	}
	if input.Country != "" {
		vendor.Country = input.Country
	}
	if input.PostalCode != "" {
		vendor.PostalCode = input.PostalCode
	}
	if input.TaxID != "" {
		vendor.TaxID = input.TaxID
	}
	if input.PaymentTerms != "" {
		vendor.PaymentTerms = input.PaymentTerms
	}

	if err := s.repo.Update(ctx, vendor); err != nil {
		return nil, err
	}

	return vendor, nil
}

func (s *VendorService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}
