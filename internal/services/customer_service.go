package services

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"mime/multipart"
	 numbers "strconv"
	"strings"
	"time"

	"github.com/cyfernova/invoice-backend/internal/models"
	"github.com/cyfernova/invoice-backend/internal/repositories"
	"github.com/cyfernova/invoice-backend/internal/utils"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ParseDecimal parses a string to decimal.Decimal
func ParseDecimal(s string) (decimal.Decimal, error) {
	return decimal.NewFromString(s)
}

// CustomerService handles customer business logic
type CustomerService struct {
	customerRepo repositories.CustomerRepository
}

// NewCustomerService creates a new customer service
func NewCustomerService(customerRepo repositories.CustomerRepository) *CustomerService {
	return &CustomerService{
		customerRepo: customerRepo,
	}
}

// CreateCustomerRequest represents customer creation request
type CreateCustomerRequest struct {
	BusinessID   string                  `json:"business_id" validate:"required,uuid"`
	Name         string                  `json:"name" validate:"required,min=2,max=255"`
	Type         models.CustomerType     `json:"type" validate:"required"`
	Phone        string                  `json:"phone" validate:"omitempty,max=20"`
	Email        string                  `json:"email" validate:"omitempty,email,max=255"`
	Address      string                  `json:"address" validate:"omitempty,max=500"`
	City         string                  `json:"city" validate:"omitempty,max=100"`
	State        string                  `json:"state" validate:"omitempty,max=100"`
	Pincode      string                  `json:"pincode" validate:"omitempty,max=20"`
	GSTIN        string                  `json:"gstin" validate:"omitempty,len=15"`
	PAN          string                  `json:"pan" validate:"omitempty,len=10"`
	CreditLimit  decimal.Decimal         `json:"credit_limit"`
	CreditPeriod int                     `json:"credit_period" validate:"min=0,max=365"`
}

// UpdateCustomerRequest represents customer update request
type UpdateCustomerRequest struct {
	Name         *string                 `json:"name" validate:"omitempty,min=2,max=255"`
	Type         *models.CustomerType    `json:"type" validate:"omitempty"`
	Phone        *string                 `json:"phone" validate:"omitempty,max=20"`
	Email        *string                 `json:"email" validate:"omitempty,email,max=255"`
	Address      *string                 `json:"address" validate:"omitempty,max=500"`
	City         *string                 `json:"city" validate:"omitempty,max=100"`
	State        *string                 `json:"state" validate:"omitempty,max=100"`
	Pincode      *string                 `json:"pincode" validate:"omitempty,max=20"`
	GSTIN        *string                 `json:"gstin" validate:"omitempty,len=15"`
	PAN          *string                 `json:"pan" validate:"omitempty,len=10"`
	CreditLimit  *decimal.Decimal        `json:"credit_limit"`
	CreditPeriod *int                    `json:"credit_period" validate:"omitempty,min=0,max=365"`
	IsActive     *bool                   `json:"is_active"`
}

// CustomerResponse represents customer response
type CustomerResponse struct {
	ID            string              `json:"id"`
	BusinessID    string              `json:"business_id"`
	Name          string              `json:"name"`
	Type          models.CustomerType `json:"type"`
	Phone         string              `json:"phone"`
	Email         string              `json:"email"`
	Address       string              `json:"address"`
	City          string              `json:"city"`
	State         string              `json:"state"`
	Pincode       string              `json:"pincode"`
	GSTIN         string              `json:"gstin"`
	PAN           string              `json:"pan"`
	CreditLimit   decimal.Decimal     `json:"credit_limit"`
	CreditPeriod  int                 `json:"credit_period"`
	Balance       decimal.Decimal     `json:"balance"`
	IsActive      bool                `json:"is_active"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

// UpdateBalanceRequest represents balance update request
type UpdateBalanceRequest struct {
	Amount           decimal.Decimal `json:"amount" validate:"required"`
	Description      string          `json:"description"`
	TransactionType  string          `json:"transaction_type" validate:"required,oneof=credit debit payment_received invoice_created adjustment"`
	ReferenceID      *string         `json:"reference_id"`
	ReferenceType    string          `json:"reference_type"`
}

// Create creates a new customer
func (s *CustomerService) Create(ctx context.Context, req *CreateCustomerRequest, createdBy string) (*CustomerResponse, error) {
	// Validate business ID
	businessID, err := uuid.Parse(req.BusinessID)
	if err != nil {
		return nil, utils.NewAppError(400, "Invalid business ID", err)
	}

	// Check duplicate
	isDuplicate, err := s.customerRepo.CheckDuplicate(ctx, req.BusinessID, req.GSTIN, req.PAN, req.Email)
	if err != nil {
		return nil, err
	}
	if isDuplicate {
		return nil, utils.NewAppError(409, "Customer with same GSTIN, PAN, or Email already exists", nil)
	}

	// Validate customer type
	if !models.IsValidCustomerType(req.Type) {
		return nil, utils.NewAppError(400, "Invalid customer type", nil)
	}

	// Create customer
	customer := &models.Customer{
		BusinessID:   businessID,
		Name:         req.Name,
		Type:         req.Type,
		Phone:        req.Phone,
		Email:        req.Email,
		Address:      req.Address,
		City:         req.City,
		State:        req.State,
		Pincode:      req.Pincode,
		GSTIN:        req.GSTIN,
		PAN:          req.PAN,
		CreditLimit:   req.CreditLimit,
		CreditPeriod:  req.CreditPeriod,
		Balance:      decimal.Zero,
		IsActive:     true,
	}

	if createdBy != "" {
		if id, err := uuid.Parse(createdBy); err == nil {
			customer.CreatedBy = id
		}
	}

	// Validate GSTIN and PAN
	if !customer.ValidateGSTIN() {
		return nil, utils.NewAppError(400, "Invalid GSTIN format", nil)
	}
	if !customer.ValidatePAN() {
		return nil, utils.NewAppError(400, "Invalid PAN format", nil)
	}

	if err := s.customerRepo.Create(ctx, customer); err != nil {
		return nil, err
	}

	return s.toResponse(customer), nil
}

// GetByID retrieves a customer by ID
func (s *CustomerService) GetByID(ctx context.Context, id string) (*CustomerResponse, error) {
	customer, err := s.customerRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if customer == nil {
		return nil, utils.ErrNotFound
	}
	return s.toResponse(customer), nil
}

// Update updates an existing customer
func (s *CustomerService) Update(ctx context.Context, id string, req *UpdateCustomerRequest) (*CustomerResponse, error) {
	customer, err := s.customerRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if customer == nil {
		return nil, utils.ErrNotFound
	}

	// Update fields
	if req.Name != nil {
		customer.Name = *req.Name
	}
	if req.Type != nil {
		if !models.IsValidCustomerType(*req.Type) {
			return nil, utils.NewAppError(400, "Invalid customer type", nil)
		}
		customer.Type = *req.Type
	}
	if req.Phone != nil {
		customer.Phone = *req.Phone
	}
	if req.Email != nil {
		customer.Email = *req.Email
	}
	if req.Address != nil {
		customer.Address = *req.Address
	}
	if req.City != nil {
		customer.City = *req.City
	}
	if req.State != nil {
		customer.State = *req.State
	}
	if req.Pincode != nil {
		customer.Pincode = *req.Pincode
	}
	if req.GSTIN != nil {
		customer.GSTIN = *req.GSTIN
		if !customer.ValidateGSTIN() {
			return nil, utils.NewAppError(400, "Invalid GSTIN format", nil)
		}
	}
	if req.PAN != nil {
		customer.PAN = *req.PAN
		if !customer.ValidatePAN() {
			return nil, utils.NewAppError(400, "Invalid PAN format", nil)
		}
	}
	if req.CreditLimit != nil {
		customer.CreditLimit = *req.CreditLimit
	}
	if req.CreditPeriod != nil {
		customer.CreditPeriod = *req.CreditPeriod
	}
	if req.IsActive != nil {
		customer.IsActive = *req.IsActive
	}

	if err := s.customerRepo.Update(ctx, customer); err != nil {
		return nil, err
	}

	return s.toResponse(customer), nil
}

// Delete soft deletes a customer
func (s *CustomerService) Delete(ctx context.Context, id string) error {
	customer, err := s.customerRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if customer == nil {
		return utils.ErrNotFound
	}

	return s.customerRepo.Delete(ctx, id)
}

// List retrieves customers for a business with pagination
func (s *CustomerService) List(ctx context.Context, businessID string, page, perPage int) ([]*CustomerResponse, int64, error) {
	customers, total, err := s.customerRepo.GetByBusinessID(ctx, businessID, page, perPage)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*CustomerResponse, len(customers))
	for i, c := range customers {
		responses[i] = s.toResponse(c)
	}

	return responses, total, nil
}

// Search searches customers by name
func (s *CustomerService) Search(ctx context.Context, businessID, query string, page, perPage int) ([]*CustomerResponse, int64, error) {
	customers, total, err := s.customerRepo.SearchByName(ctx, businessID, query, page, perPage)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*CustomerResponse, len(customers))
	for i, c := range customers {
		responses[i] = s.toResponse(c)
	}

	return responses, total, nil
}

// FilterByType filters customers by type
func (s *CustomerService) FilterByType(ctx context.Context, businessID string, customerType models.CustomerType, page, perPage int) ([]*CustomerResponse, int64, error) {
	if !models.IsValidCustomerType(customerType) {
		return nil, 0, utils.NewAppError(400, "Invalid customer type", nil)
	}

	customers, total, err := s.customerRepo.FilterByType(ctx, businessID, customerType, page, perPage)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*CustomerResponse, len(customers))
	for i, c := range customers {
		responses[i] = s.toResponse(c)
	}

	return responses, total, nil
}

// UpdateBalance updates customer balance
func (s *CustomerService) UpdateBalance(ctx context.Context, id string, req *UpdateBalanceRequest) error {
	// Validate transaction type
	if !models.IsValidTransactionType(req.TransactionType) {
		return utils.NewAppError(400, "Invalid transaction type", nil)
	}

	// Determine amount direction based on transaction type
	amount := req.Amount
	switch req.TransactionType {
	case "debit", "payment_received":
		amount = amount.Neg()
	}

	return s.customerRepo.UpdateBalance(ctx, id, amount, req.TransactionType, req.Description, req.ReferenceID, req.ReferenceType)
}

// GetOutstanding retrieves customers with outstanding balances
func (s *CustomerService) GetOutstanding(ctx context.Context, businessID string) ([]*CustomerResponse, error) {
	customers, err := s.customerRepo.GetOutstanding(ctx, businessID)
	if err != nil {
		return nil, err
	}

	responses := make([]*CustomerResponse, len(customers))
	for i, c := range customers {
		responses[i] = s.toResponse(c)
	}

	return responses, nil
}

// GetStats retrieves customer statistics
func (s *CustomerService) GetStats(ctx context.Context, businessID string) (*repositories.CustomerStats, error) {
	return s.customerRepo.GetStats(ctx, businessID)
}

// ImportCSV imports customers from CSV file
func (s *CustomerService) ImportCSV(ctx context.Context, businessID string, file *multipart.FileHeader) ([]*CustomerResponse, []string, error) {
	// Open the uploaded file
	src, err := file.Open()
	if err != nil {
		return nil, nil, err
	}
	defer src.Close()

	// Parse CSV
	reader := csv.NewReader(src)
	var records [][]string

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		records = append(records, record)
	}

	if len(records) == 0 {
		return nil, nil, utils.NewAppError(400, "CSV file is empty", nil)
	}

	// Expected columns: name, type, phone, email, address, city, state, pincode, gstin, pan, credit_limit, credit_period
	var customers []*CustomerResponse
	var errors []string

	for i, record := range records {
		// Skip header row if present
		if i == 0 && len(record) > 0 && strings.ToLower(strings.TrimSpace(record[0])) == "name" {
			continue
		}

		if len(record) < 12 {
			errors = append(errors, fmt.Sprintf("Row %d: Insufficient columns", i+1))
			continue
		}

		// Parse record
		req := &CreateCustomerRequest{
			BusinessID: businessID,
			Name:       strings.TrimSpace(record[0]),
		}

		// Parse type
		customerType := strings.ToLower(strings.TrimSpace(record[1]))
		switch customerType {
		case "retail", "wholesale", "b2b", "government":
			req.Type = models.CustomerType(customerType)
		default:
			req.Type = models.CustomerTypeOther
		}

		if len(record) > 2 {
			req.Phone = strings.TrimSpace(record[2])
		}
		if len(record) > 3 {
			req.Email = strings.TrimSpace(record[3])
		}
		if len(record) > 4 {
			req.Address = strings.TrimSpace(record[4])
		}
		if len(record) > 5 {
			req.City = strings.TrimSpace(record[5])
		}
		if len(record) > 6 {
			req.State = strings.TrimSpace(record[6])
		}
		if len(record) > 7 {
			req.Pincode = strings.TrimSpace(record[7])
		}
		if len(record) > 8 {
			req.GSTIN = strings.TrimSpace(record[8])
		}
		if len(record) > 9 {
			req.PAN = strings.TrimSpace(record[9])
		}
		if len(record) > 10 && record[10] != "" {
			if creditLimit, err := decimal.NewFromString(record[10]); err == nil {
				req.CreditLimit = creditLimit
			}
		}
		if len(record) > 11 && record[11] != "" {
			if creditPeriod, err := numbers.Atoi(record[11]); err == nil {
				req.CreditPeriod = creditPeriod
			}
		}

		// Create customer
		customer, err := s.Create(ctx, req, "")
		if err != nil {
			errors = append(errors, fmt.Sprintf("Row %d: %s", i+1, err.Error()))
			continue
		}

		customers = append(customers, customer)
	}

	return customers, errors, nil
}

// ExportCSV exports customers to CSV
func (s *CustomerService) ExportCSV(ctx context.Context, businessID string) ([]byte, error) {
	customers, _, err := s.customerRepo.GetByBusinessID(ctx, businessID, 1, 10000)
	if err != nil {
		return nil, err
	}

	// Create CSV writer
	var buf strings.Builder
	writer := csv.NewWriter(&buf)

	// Write header
	header := []string{"Name", "Type", "Phone", "Email", "Address", "City", "State", "Pincode", "GSTIN", "PAN", "Credit Limit", "Credit Period", "Balance", "Is Active"}
	writer.Write(header)

	// Write data
	for _, c := range customers {
		record := []string{
			c.Name,
			string(c.Type),
			c.Phone,
			c.Email,
			c.Address,
			c.City,
			c.State,
			c.Pincode,
			c.GSTIN,
			c.PAN,
			c.CreditLimit.String(),
			numbers.Itoa(c.CreditPeriod),
			c.Balance.String(),
			numbers.FormatBool(c.IsActive),
		}
		writer.Write(record)
	}

	writer.Flush()
	return []byte(buf.String()), writer.Error()
}

// CheckDuplicate checks for duplicate customer
func (s *CustomerService) CheckDuplicate(ctx context.Context, businessID, gstin, pan, email string) (bool, error) {
	return s.customerRepo.CheckDuplicate(ctx, businessID, gstin, pan, email)
}

func (s *CustomerService) toResponse(customer *models.Customer) *CustomerResponse {
	return &CustomerResponse{
		ID:           customer.ID.String(),
		BusinessID:   customer.BusinessID.String(),
		Name:         customer.Name,
		Type:         customer.Type,
		Phone:        customer.Phone,
		Email:        customer.Email,
		Address:      customer.Address,
		City:         customer.City,
		State:        customer.State,
		Pincode:      customer.Pincode,
		GSTIN:        customer.GSTIN,
		PAN:          customer.PAN,
		CreditLimit:  customer.CreditLimit,
		CreditPeriod: customer.CreditPeriod,
		Balance:      customer.Balance,
		IsActive:     customer.IsActive,
		CreatedAt:    customer.CreatedAt,
		UpdatedAt:    customer.UpdatedAt,
	}
}
