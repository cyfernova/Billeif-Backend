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
	log := logger.FromContext(ctx).With("service", "vendor", "operation", "create", "business_id", input.BusinessID)
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
		log.Error("failed to create vendor", "error", err)
		return nil, fmt.Errorf("failed to create vendor: %w", err)
	}

	log.Info("vendor created", "vendor_id", vendor.ID)
	return vendor, nil
}

func (s *VendorService) Get(ctx context.Context, id string) (*models.Vendor, error) {
	log := logger.FromContext(ctx).With("service", "vendor", "operation", "get", "vendor_id", id)
	vendor, err := s.repo.GetByID(ctx, id)
	if err != nil {
		log.Error("failed to get vendor", "error", err)
		return nil, err
	}
	return vendor, nil
}

func (s *VendorService) List(ctx context.Context, businessID string, page, limit int) ([]*models.Vendor, int64, error) {
	log := logger.FromContext(ctx).With("service", "vendor", "operation", "list", "business_id", businessID, "page", page, "limit", limit)
	vendors, total, err := s.repo.GetByBusinessID(ctx, businessID, page, limit)
	if err != nil {
		log.Error("failed to list vendors", "error", err)
		return nil, 0, err
	}
	log.Debug("listed vendors", "count", len(vendors), "total", total)
	return vendors, total, nil
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
	log := logger.FromContext(ctx).With("service", "vendor", "operation", "update", "vendor_id", id)
	vendor, err := s.repo.GetByID(ctx, id)
	if err != nil {
		log.Error("failed to load vendor for update", "error", err)
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
		log.Error("failed to update vendor", "error", err)
		return nil, err
	}

	log.Info("vendor updated", "vendor_id", vendor.ID)
	return vendor, nil
}

func (s *VendorService) Delete(ctx context.Context, id string) error {
	log := logger.FromContext(ctx).With("service", "vendor", "operation", "delete", "vendor_id", id)
	if err := s.repo.Delete(ctx, id); err != nil {
		log.Error("failed to delete vendor", "error", err)
		return err
	}
	log.Info("vendor deleted", "vendor_id", id)
	return nil
}
