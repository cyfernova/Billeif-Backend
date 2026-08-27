package services

import (
	"context"
	"fmt"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type VendorService struct {
	repo        interfaces.VendorRepository
	permissions PermissionChecker
	log         *logger.Logger
}

func NewVendorService(repo interfaces.VendorRepository, permissions PermissionChecker, log *logger.Logger) *VendorService {
	return &VendorService{repo: repo, permissions: permissions, log: log}
}

type CreateVendorInput struct {
	BusinessID              string                 `json:"business_id,omitempty"`
	Name                    string                 `json:"name" binding:"required,min=2"`
	Email                   string                 `json:"email" binding:"required,email"`
	Phone                   string                 `json:"phone"`
	Address                 string                 `json:"address"`
	City                    string                 `json:"city"`
	State                   string                 `json:"state"`
	Country                 string                 `json:"country"`
	PostalCode              string                 `json:"postal_code"`
	TaxID                   string                 `json:"tax_id"`
	GSTIN                   string                 `json:"gstin"`
	PAN                     string                 `json:"pan"`
	CompanyName             string                 `json:"company_name"`
	StateCode               string                 `json:"state_code"`
	BillingAddressJSON      map[string]interface{} `json:"billing_address_json,omitempty"`
	ShippingAddressJSON     map[string]interface{} `json:"shipping_address_json,omitempty"`
	WithholdingDefaultsJSON map[string]interface{} `json:"withholding_defaults_json,omitempty"`
	PaymentTerms            string                 `json:"payment_terms"`
}

func (s *VendorService) Create(ctx context.Context, input CreateVendorInput) (*models.Vendor, error) {
	if err := requireMutationPermission(ctx, s.permissions, input.BusinessID, PermissionVendorsCreate); err != nil {
		return nil, err
	}
	log := logger.FromContext(ctx).With("service", "vendor", "operation", "create", "business_id", input.BusinessID)
	vendor := &models.Vendor{
		BusinessID:              input.BusinessID,
		Name:                    input.Name,
		Email:                   input.Email,
		Phone:                   input.Phone,
		Address:                 input.Address,
		City:                    input.City,
		State:                   input.State,
		Country:                 input.Country,
		PostalCode:              input.PostalCode,
		TaxID:                   input.TaxID,
		GSTIN:                   input.GSTIN,
		PAN:                     input.PAN,
		CompanyName:             input.CompanyName,
		StateCode:               input.StateCode,
		BillingJSON:             mustMarshalMap(input.BillingAddressJSON),
		ShippingJSON:            mustMarshalMap(input.ShippingAddressJSON),
		WithholdingDefaultsJSON: mustMarshalMap(input.WithholdingDefaultsJSON),
		PaymentTerms:            input.PaymentTerms,
	}

	if err := s.repo.Create(ctx, vendor); err != nil {
		log.Error("failed to create vendor", "error", err)
		return nil, fmt.Errorf("failed to create vendor: %w", err)
	}

	log.Info("vendor created", "vendor_id", vendor.ID)
	return vendor, nil
}

func (s *VendorService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Vendor, error) {
	return s.repo.GetByID(ctx, id, businessID)
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
	Name                    string                 `json:"name"`
	Email                   string                 `json:"email"`
	Phone                   string                 `json:"phone"`
	Address                 string                 `json:"address"`
	City                    string                 `json:"city"`
	State                   string                 `json:"state"`
	Country                 string                 `json:"country"`
	PostalCode              string                 `json:"postal_code"`
	TaxID                   string                 `json:"tax_id"`
	GSTIN                   string                 `json:"gstin"`
	PAN                     string                 `json:"pan"`
	CompanyName             string                 `json:"company_name"`
	StateCode               string                 `json:"state_code"`
	BillingAddressJSON      map[string]interface{} `json:"billing_address_json,omitempty"`
	ShippingAddressJSON     map[string]interface{} `json:"shipping_address_json,omitempty"`
	WithholdingDefaultsJSON map[string]interface{} `json:"withholding_defaults_json,omitempty"`
	PaymentTerms            string                 `json:"payment_terms"`
}

func (s *VendorService) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdateVendorInput) (*models.Vendor, error) {
	if err := requireMutationPermission(ctx, s.permissions, businessID, PermissionVendorsUpdate); err != nil {
		return nil, err
	}
	log := logger.FromContext(ctx).With("service", "vendor", "operation", "update", "vendor_id", id, "business_id", businessID)
	vendor, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		log.Error("failed to load vendor for scoped update", "error", err)
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
	if input.GSTIN != "" {
		vendor.GSTIN = input.GSTIN
	}
	if input.PAN != "" {
		vendor.PAN = input.PAN
	}
	if input.CompanyName != "" {
		vendor.CompanyName = input.CompanyName
	}
	if input.StateCode != "" {
		vendor.StateCode = input.StateCode
	}
	if input.BillingAddressJSON != nil {
		vendor.BillingJSON = mustMarshalMap(input.BillingAddressJSON)
	}
	if input.ShippingAddressJSON != nil {
		vendor.ShippingJSON = mustMarshalMap(input.ShippingAddressJSON)
	}
	if input.WithholdingDefaultsJSON != nil {
		vendor.WithholdingDefaultsJSON = mustMarshalMap(input.WithholdingDefaultsJSON)
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

func (s *VendorService) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	if err := requireMutationPermission(ctx, s.permissions, businessID, PermissionVendorsDelete); err != nil {
		return err
	}
	log := logger.FromContext(ctx).With("service", "vendor", "operation", "delete", "vendor_id", id, "business_id", businessID)
	vendor, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		log.Error("failed to load vendor for scoped delete", "error", err)
		return err
	}
	if err := s.repo.Delete(ctx, vendor.ID); err != nil {
		log.Error("failed to delete vendor", "error", err)
		return err
	}
	log.Info("vendor deleted", "vendor_id", vendor.ID)
	return nil
}
