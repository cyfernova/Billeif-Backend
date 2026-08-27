package services

import (
	"context"
	"fmt"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type CustomerService struct {
	repo        interfaces.CustomerRepository
	permissions PermissionChecker
	log         *logger.Logger
}

func NewCustomerService(repo interfaces.CustomerRepository, permissions PermissionChecker, log *logger.Logger) *CustomerService {
	return &CustomerService{repo: repo, permissions: permissions, log: log}
}

type CreateCustomerInput struct {
	BusinessID              string                 `json:"business_id,omitempty"`
	Name                    string                 `json:"name" binding:"required,min=2"`
	Email                   string                 `json:"email" binding:"required,email"`
	Phone                   string                 `json:"phone"`
	Address                 string                 `json:"address"`
	BillingAddress          string                 `json:"billing_address"`
	City                    string                 `json:"city"`
	State                   string                 `json:"state"`
	Country                 string                 `json:"country"`
	ZipCode                 string                 `json:"zip_code"`
	Pincode                 string                 `json:"pincode"`
	TaxID                   string                 `json:"tax_id"`
	GSTIN                   string                 `json:"gstin"`
	PAN                     string                 `json:"pan"`
	CompanyName             string                 `json:"company_name"`
	StateCode               string                 `json:"state_code"`
	BillingAddressJSON      map[string]interface{} `json:"billing_address_json,omitempty"`
	ShippingAddressJSON     map[string]interface{} `json:"shipping_address_json,omitempty"`
	WithholdingDefaultsJSON map[string]interface{} `json:"withholding_defaults_json,omitempty"`
	CreditLimit             float64                `json:"credit_limit"`
	PaymentTerms            int                    `json:"payment_terms"`
}

func (s *CustomerService) Create(ctx context.Context, input CreateCustomerInput) (*models.Customer, error) {
	if err := requireMutationPermission(ctx, s.permissions, input.BusinessID, PermissionCustomersCreate); err != nil {
		return nil, err
	}
	return s.create(ctx, input)
}

func (s *CustomerService) create(ctx context.Context, input CreateCustomerInput) (*models.Customer, error) {
	address := firstNonEmpty(input.Address, input.BillingAddress)
	postalCode := firstNonEmpty(input.ZipCode, input.Pincode)
	customer := &models.Customer{
		BusinessID:              input.BusinessID,
		Name:                    input.Name,
		Email:                   input.Email,
		Phone:                   input.Phone,
		Address:                 address,
		City:                    input.City,
		State:                   input.State,
		Country:                 input.Country,
		PostalCode:              postalCode,
		TaxID:                   input.TaxID,
		GSTIN:                   input.GSTIN,
		PAN:                     input.PAN,
		CompanyName:             input.CompanyName,
		StateCode:               input.StateCode,
		BillingJSON:             mustMarshalMap(input.BillingAddressJSON),
		ShippingJSON:            mustMarshalMap(input.ShippingAddressJSON),
		WithholdingDefaultsJSON: mustMarshalMap(input.WithholdingDefaultsJSON),
		CreditLimit:             input.CreditLimit,
	}

	if err := s.repo.Create(ctx, customer); err != nil {
		return nil, fmt.Errorf("failed to create customer: %w", err)
	}

	return customer, nil
}

func (s *CustomerService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Customer, error) {
	return s.repo.GetByID(ctx, id, businessID)
}

func (s *CustomerService) List(ctx context.Context, businessID string, page, limit int) ([]*models.Customer, int64, error) {
	return s.repo.GetByBusinessID(ctx, businessID, page, limit)
}

type UpdateCustomerInput struct {
	Name                    string                 `json:"name"`
	Email                   string                 `json:"email"`
	Phone                   string                 `json:"phone"`
	Address                 string                 `json:"address"`
	BillingAddress          string                 `json:"billing_address"`
	City                    string                 `json:"city"`
	State                   string                 `json:"state"`
	Country                 string                 `json:"country"`
	ZipCode                 string                 `json:"zip_code"`
	Pincode                 string                 `json:"pincode"`
	TaxID                   string                 `json:"tax_id"`
	GSTIN                   string                 `json:"gstin"`
	PAN                     string                 `json:"pan"`
	CompanyName             string                 `json:"company_name"`
	StateCode               string                 `json:"state_code"`
	BillingAddressJSON      map[string]interface{} `json:"billing_address_json,omitempty"`
	ShippingAddressJSON     map[string]interface{} `json:"shipping_address_json,omitempty"`
	WithholdingDefaultsJSON map[string]interface{} `json:"withholding_defaults_json,omitempty"`
	CreditLimit             float64                `json:"credit_limit"`
	PaymentTerms            int                    `json:"payment_terms"`
}

func (s *CustomerService) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdateCustomerInput) (*models.Customer, error) {
	if err := requireMutationPermission(ctx, s.permissions, businessID, PermissionCustomersUpdate); err != nil {
		return nil, err
	}
	customer, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return nil, err
	}

	if input.Name != "" {
		customer.Name = input.Name
	}
	if input.Email != "" {
		customer.Email = input.Email
	}
	if input.Phone != "" {
		customer.Phone = input.Phone
	}
	if address := firstNonEmpty(input.Address, input.BillingAddress); address != "" {
		customer.Address = address
	}
	if input.City != "" {
		customer.City = input.City
	}
	if input.State != "" {
		customer.State = input.State
	}
	if input.Country != "" {
		customer.Country = input.Country
	}
	if postalCode := firstNonEmpty(input.ZipCode, input.Pincode); postalCode != "" {
		customer.PostalCode = postalCode
	}
	if input.TaxID != "" {
		customer.TaxID = input.TaxID
	}
	if input.GSTIN != "" {
		customer.GSTIN = input.GSTIN
	}
	if input.PAN != "" {
		customer.PAN = input.PAN
	}
	if input.CompanyName != "" {
		customer.CompanyName = input.CompanyName
	}
	if input.StateCode != "" {
		customer.StateCode = input.StateCode
	}
	if input.BillingAddressJSON != nil {
		customer.BillingJSON = mustMarshalMap(input.BillingAddressJSON)
	}
	if input.ShippingAddressJSON != nil {
		customer.ShippingJSON = mustMarshalMap(input.ShippingAddressJSON)
	}
	if input.WithholdingDefaultsJSON != nil {
		customer.WithholdingDefaultsJSON = mustMarshalMap(input.WithholdingDefaultsJSON)
	}
	if input.CreditLimit > 0 {
		customer.CreditLimit = input.CreditLimit
	}

	if err := s.repo.Update(ctx, customer); err != nil {
		return nil, err
	}

	return customer, nil
}

func (s *CustomerService) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	if err := requireMutationPermission(ctx, s.permissions, businessID, PermissionCustomersDelete); err != nil {
		return err
	}
	customer, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, customer.ID)
}

func (s *CustomerService) Import(ctx context.Context, businessID string, customers []CreateCustomerInput) (int, error) {
	if err := requireMutationPermission(ctx, s.permissions, businessID, PermissionCustomersCreate); err != nil {
		return 0, err
	}
	count := 0
	for _, c := range customers {
		c.BusinessID = businessID
		if _, err := s.create(ctx, c); err != nil {
			s.log.Warn("failed to import customer", "name", c.Name, "error", err)
			continue
		}
		count++
	}
	return count, nil
}

const maxExportRecords = 5000

func (s *CustomerService) Export(ctx context.Context, businessID string) ([]*models.Customer, error) {
	customers, _, err := s.repo.GetByBusinessID(ctx, businessID, 1, maxExportRecords)
	return customers, err
}
