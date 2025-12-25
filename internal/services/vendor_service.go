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

// VendorService handles vendor business logic
type VendorService struct {
	vendorRepo repositories.VendorRepository
}

// NewVendorService creates a new vendor service
func NewVendorService(vendorRepo repositories.VendorRepository) *VendorService {
	return &VendorService{
		vendorRepo: vendorRepo,
	}
}

// CreateVendorRequest represents vendor creation request
type CreateVendorRequest struct {
	BusinessID   string                `json:"business_id" validate:"required,uuid"`
	Name         string                `json:"name" validate:"required,min=2,max=255"`
	Type         models.VendorType     `json:"type" validate:"required"`
	Phone        string                `json:"phone" validate:"omitempty,max=20"`
	Email        string                `json:"email" validate:"omitempty,email,max=255"`
	Address      string                `json:"address" validate:"omitempty,max=500"`
	City         string                `json:"city" validate:"omitempty,max=100"`
	State        string                `json:"state" validate:"omitempty,max=100"`
	Pincode      string                `json:"pincode" validate:"omitempty,max=20"`
	GSTIN        string                `json:"gstin" validate:"omitempty,len=15"`
	PAN          string                `json:"pan" validate:"omitempty,len=10"`
	CreditLimit  decimal.Decimal       `json:"credit_limit"`
	CreditPeriod int                   `json:"credit_period" validate:"min=0,max=365"`
	PaymentTerms string                `json:"payment_terms" validate:"omitempty,max=100"`
	BankAccountNo string               `json:"bank_account_no" validate:"omitempty,max=50"`
	BankIFSC      string               `json:"bank_ifsc" validate:"omitempty,max=20"`
	BankName      string               `json:"bank_name" validate:"omitempty,max=100"`
}

// UpdateVendorRequest represents vendor update request
type UpdateVendorRequest struct {
	Name         *string               `json:"name" validate:"omitempty,min=2,max=255"`
	Type         *models.VendorType    `json:"type" validate:"omitempty"`
	Phone        *string               `json:"phone" validate:"omitempty,max=20"`
	Email        *string               `json:"email" validate:"omitempty,email,max=255"`
	Address      *string               `json:"address" validate:"omitempty,max=500"`
	City         *string               `json:"city" validate:"omitempty,max=100"`
	State        *string               `json:"state" validate:"omitempty,max=100"`
	Pincode      *string               `json:"pincode" validate:"omitempty,max=20"`
	GSTIN        *string               `json:"gstin" validate:"omitempty,len=15"`
	PAN          *string               `json:"pan" validate:"omitempty,len=10"`
	CreditLimit  *decimal.Decimal      `json:"credit_limit"`
	CreditPeriod *int                  `json:"credit_period" validate:"omitempty,min=0,max=365"`
	PaymentTerms *string               `json:"payment_terms" validate:"omitempty,max=100"`
	BankAccountNo *string               `json:"bank_account_no" validate:"omitempty,max=50"`
	BankIFSC      *string               `json:"bank_ifsc" validate:"omitempty,max=20"`
	BankName      *string               `json:"bank_name" validate:"omitempty,max=100"`
	IsActive      *bool                 `json:"is_active"`
}

// VendorResponse represents vendor response
type VendorResponse struct {
	ID            string            `json:"id"`
	BusinessID    string            `json:"business_id"`
	Name          string            `json:"name"`
	Type          models.VendorType `json:"type"`
	Phone         string            `json:"phone"`
	Email         string            `json:"email"`
	Address       string            `json:"address"`
	City          string            `json:"city"`
	State         string            `json:"state"`
	Pincode       string            `json:"pincode"`
	GSTIN         string            `json:"gstin"`
	PAN           string            `json:"pan"`
	CreditLimit   decimal.Decimal   `json:"credit_limit"`
	CreditPeriod  int               `json:"credit_period"`
	Balance       decimal.Decimal   `json:"balance"`
	PaymentTerms  string            `json:"payment_terms"`
	BankAccountNo string            `json:"bank_account_no"`
	BankIFSC      string            `json:"bank_ifsc"`
	BankName      string            `json:"bank_name"`
	IsActive      bool              `json:"is_active"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// UpdateBalanceRequest represents balance update request
type VendorUpdateBalanceRequest struct {
	Amount           decimal.Decimal `json:"amount" validate:"required"`
	Description      string          `json:"description"`
	TransactionType  string          `json:"transaction_type" validate:"required,oneof=credit debit payment_made purchase_created adjustment"`
	ReferenceID      *string         `json:"reference_id"`
	ReferenceType    string          `json:"reference_type"`
}

// Create creates a new vendor
func (s *VendorService) Create(ctx context.Context, req *CreateVendorRequest, createdBy string) (*VendorResponse, error) {
	businessID, err := uuid.Parse(req.BusinessID)
	if err != nil {
		return nil, utils.NewAppError(400, "Invalid business ID", err)
	}

	isDuplicate, err := s.vendorRepo.CheckDuplicate(ctx, req.BusinessID, req.GSTIN, req.PAN, req.Email)
	if err != nil {
		return nil, err
	}
	if isDuplicate {
		return nil, utils.NewAppError(409, "Vendor with same GSTIN, PAN, or Email already exists", nil)
	}

	if !models.IsValidVendorType(req.Type) {
		return nil, utils.NewAppError(400, "Invalid vendor type", nil)
	}

	vendor := &models.Vendor{
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
		PaymentTerms: req.PaymentTerms,
		BankAccountNo: req.BankAccountNo,
		BankIFSC:      req.BankIFSC,
		BankName:      req.BankName,
		IsActive:     true,
	}

	if createdBy != "" {
		if id, err := uuid.Parse(createdBy); err == nil {
			vendor.CreatedBy = id
		}
	}

	if !vendor.ValidateGSTIN() {
		return nil, utils.NewAppError(400, "Invalid GSTIN format", nil)
	}
	if !vendor.ValidatePAN() {
		return nil, utils.NewAppError(400, "Invalid PAN format", nil)
	}

	if err := s.vendorRepo.Create(ctx, vendor); err != nil {
		return nil, err
	}

	return s.toResponse(vendor), nil
}

// GetByID retrieves a vendor by ID
func (s *VendorService) GetByID(ctx context.Context, id string) (*VendorResponse, error) {
	vendor, err := s.vendorRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if vendor == nil {
		return nil, utils.ErrNotFound
	}
	return s.toResponse(vendor), nil
}

// Update updates an existing vendor
func (s *VendorService) Update(ctx context.Context, id string, req *UpdateVendorRequest) (*VendorResponse, error) {
	vendor, err := s.vendorRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if vendor == nil {
		return nil, utils.ErrNotFound
	}

	if req.Name != nil {
		vendor.Name = *req.Name
	}
	if req.Type != nil {
		if !models.IsValidVendorType(*req.Type) {
			return nil, utils.NewAppError(400, "Invalid vendor type", nil)
		}
		vendor.Type = *req.Type
	}
	if req.Phone != nil {
		vendor.Phone = *req.Phone
	}
	if req.Email != nil {
		vendor.Email = *req.Email
	}
	if req.Address != nil {
		vendor.Address = *req.Address
	}
	if req.City != nil {
		vendor.City = *req.City
	}
	if req.State != nil {
		vendor.State = *req.State
	}
	if req.Pincode != nil {
		vendor.Pincode = *req.Pincode
	}
	if req.GSTIN != nil {
		vendor.GSTIN = *req.GSTIN
		if !vendor.ValidateGSTIN() {
			return nil, utils.NewAppError(400, "Invalid GSTIN format", nil)
		}
	}
	if req.PAN != nil {
		vendor.PAN = *req.PAN
		if !vendor.ValidatePAN() {
			return nil, utils.NewAppError(400, "Invalid PAN format", nil)
		}
	}
	if req.CreditLimit != nil {
		vendor.CreditLimit = *req.CreditLimit
	}
	if req.CreditPeriod != nil {
		vendor.CreditPeriod = *req.CreditPeriod
	}
	if req.PaymentTerms != nil {
		vendor.PaymentTerms = *req.PaymentTerms
	}
	if req.BankAccountNo != nil {
		vendor.BankAccountNo = *req.BankAccountNo
	}
	if req.BankIFSC != nil {
		vendor.BankIFSC = *req.BankIFSC
	}
	if req.BankName != nil {
		vendor.BankName = *req.BankName
	}
	if req.IsActive != nil {
		vendor.IsActive = *req.IsActive
	}

	if err := s.vendorRepo.Update(ctx, vendor); err != nil {
		return nil, err
	}

	return s.toResponse(vendor), nil
}

// Delete soft deletes a vendor
func (s *VendorService) Delete(ctx context.Context, id string) error {
	vendor, err := s.vendorRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if vendor == nil {
		return utils.ErrNotFound
	}

	return s.vendorRepo.Delete(ctx, id)
}

// List retrieves vendors for a business with pagination
func (s *VendorService) List(ctx context.Context, businessID string, page, perPage int) ([]*VendorResponse, int64, error) {
	vendors, total, err := s.vendorRepo.GetByBusinessID(ctx, businessID, page, perPage)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*VendorResponse, len(vendors))
	for i, v := range vendors {
		responses[i] = s.toResponse(v)
	}

	return responses, total, nil
}

// Search searches vendors by name
func (s *VendorService) Search(ctx context.Context, businessID, query string, page, perPage int) ([]*VendorResponse, int64, error) {
	vendors, total, err := s.vendorRepo.SearchByName(ctx, businessID, query, page, perPage)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*VendorResponse, len(vendors))
	for i, v := range vendors {
		responses[i] = s.toResponse(v)
	}

	return responses, total, nil
}

// FilterByType filters vendors by type
func (s *VendorService) FilterByType(ctx context.Context, businessID string, vendorType models.VendorType, page, perPage int) ([]*VendorResponse, int64, error) {
	if !models.IsValidVendorType(vendorType) {
		return nil, 0, utils.NewAppError(400, "Invalid vendor type", nil)
	}

	vendors, total, err := s.vendorRepo.FilterByType(ctx, businessID, vendorType, page, perPage)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*VendorResponse, len(vendors))
	for i, v := range vendors {
		responses[i] = s.toResponse(v)
	}

	return responses, total, nil
}

// UpdateBalance updates vendor balance
func (s *VendorService) UpdateBalance(ctx context.Context, id string, req *VendorUpdateBalanceRequest) error {
	if !models.IsValidVendorTransactionType(req.TransactionType) {
		return utils.NewAppError(400, "Invalid transaction type", nil)
	}

	amount := req.Amount
	switch req.TransactionType {
	case "debit", "payment_made":
		amount = amount.Neg()
	}

	return s.vendorRepo.UpdateBalance(ctx, id, amount, req.TransactionType, req.Description, req.ReferenceID, req.ReferenceType)
}

// GetOutstanding retrieves vendors with outstanding balances
func (s *VendorService) GetOutstanding(ctx context.Context, businessID string) ([]*VendorResponse, error) {
	vendors, err := s.vendorRepo.GetOutstanding(ctx, businessID)
	if err != nil {
		return nil, err
	}

	responses := make([]*VendorResponse, len(vendors))
	for i, v := range vendors {
		responses[i] = s.toResponse(v)
	}

	return responses, nil
}

// GetStats retrieves vendor statistics
func (s *VendorService) GetStats(ctx context.Context, businessID string) (*repositories.VendorStats, error) {
	return s.vendorRepo.GetStats(ctx, businessID)
}

// ImportCSV imports vendors from CSV file
func (s *VendorService) ImportCSV(ctx context.Context, businessID string, file *multipart.FileHeader) ([]*VendorResponse, []string, error) {
	src, err := file.Open()
	if err != nil {
		return nil, nil, err
	}
	defer src.Close()

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

	var vendors []*VendorResponse
	var errors []string

	for i, record := range records {
		if i == 0 && len(record) > 0 && strings.ToLower(strings.TrimSpace(record[0])) == "name" {
			continue
		}

		if len(record) < 12 {
			errors = append(errors, fmt.Sprintf("Row %d: Insufficient columns", i+1))
			continue
		}

		req := &CreateVendorRequest{
			BusinessID: businessID,
			Name:       strings.TrimSpace(record[0]),
		}

		vendorType := strings.ToLower(strings.TrimSpace(record[1]))
		switch vendorType {
		case "manufacturer", "distributor", "wholesaler", "retailer", "service":
			req.Type = models.VendorType(vendorType)
		default:
			req.Type = models.VendorTypeOther
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

		vendor, err := s.Create(ctx, req, "")
		if err != nil {
			errors = append(errors, fmt.Sprintf("Row %d: %s", i+1, err.Error()))
			continue
		}

		vendors = append(vendors, vendor)
	}

	return vendors, errors, nil
}

// ExportCSV exports vendors to CSV
func (s *VendorService) ExportCSV(ctx context.Context, businessID string) ([]byte, error) {
	vendors, _, err := s.vendorRepo.GetByBusinessID(ctx, businessID, 1, 10000)
	if err != nil {
		return nil, err
	}

	var buf strings.Builder
	writer := csv.NewWriter(&buf)

	header := []string{"Name", "Type", "Phone", "Email", "Address", "City", "State", "Pincode", "GSTIN", "PAN", "Credit Limit", "Credit Period", "Balance", "Payment Terms", "Bank Name", "Is Active"}
	writer.Write(header)

	for _, v := range vendors {
		record := []string{
			v.Name,
			string(v.Type),
			v.Phone,
			v.Email,
			v.Address,
			v.City,
			v.State,
			v.Pincode,
			v.GSTIN,
			v.PAN,
			v.CreditLimit.String(),
			numbers.Itoa(v.CreditPeriod),
			v.Balance.String(),
			v.PaymentTerms,
			v.BankName,
			numbers.FormatBool(v.IsActive),
		}
		writer.Write(record)
	}

	writer.Flush()
	return []byte(buf.String()), writer.Error()
}

func (s *VendorService) toResponse(vendor *models.Vendor) *VendorResponse {
	return &VendorResponse{
		ID:            vendor.ID.String(),
		BusinessID:    vendor.BusinessID.String(),
		Name:          vendor.Name,
		Type:          vendor.Type,
		Phone:         vendor.Phone,
		Email:         vendor.Email,
		Address:       vendor.Address,
		City:          vendor.City,
		State:         vendor.State,
		Pincode:       vendor.Pincode,
		GSTIN:         vendor.GSTIN,
		PAN:           vendor.PAN,
		CreditLimit:   vendor.CreditLimit,
		CreditPeriod:  vendor.CreditPeriod,
		Balance:       vendor.Balance,
		PaymentTerms:  vendor.PaymentTerms,
		BankAccountNo: vendor.BankAccountNo,
		BankIFSC:      vendor.BankIFSC,
		BankName:      vendor.BankName,
		IsActive:      vendor.IsActive,
		CreatedAt:     vendor.CreatedAt,
		UpdatedAt:     vendor.UpdatedAt,
	}
}
