package models

import (
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// VendorType represents the type of vendor
type VendorType string

const (
	VendorTypeManufacturer VendorType = "manufacturer"
	VendorTypeDistributor  VendorType = "distributor"
	VendorTypeWholesaler   VendorType = "wholesaler"
	VendorTypeRetailer     VendorType = "retailer"
	VendorTypeService      VendorType = "service"
	VendorTypeOther        VendorType = "other"
)

// Vendor represents a vendor/supplier in the system
type Vendor struct {
	ID            uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	BusinessID    uuid.UUID      `gorm:"column:business_id;index:idx_vendors_business_id;not null" json:"business_id"`
	Name          string         `gorm:"size:255;not null;index:idx_vendors_name" json:"name"`
	Type          VendorType     `gorm:"size:50;not null;default:'other'" json:"type"`
	Phone         string         `gorm:"size:20" json:"phone,omitempty"`
	Email         string         `gorm:"size:255" json:"email,omitempty"`
	Address       string         `gorm:"type:text" json:"address,omitempty"`
	City          string         `gorm:"size:100" json:"city,omitempty"`
	State         string         `gorm:"size:100" json:"state,omitempty"`
	Pincode       string         `gorm:"size:20" json:"pincode,omitempty"`
	GSTIN         string         `gorm:"column:gstin;size:15;uniqueIndex:idx_vendors_gstin" json:"gstin,omitempty"`
	PAN           string         `gorm:"column:pan;size:10" json:"pan,omitempty"`
	CreditLimit   decimal.Decimal `gorm:"type:decimal(15,2);default:0" json:"credit_limit"`
	CreditPeriod  int            `gorm:"default:0" json:"credit_period"` // days
	Balance       decimal.Decimal `gorm:"type:decimal(15,2);default:0" json:"balance"`
	PaymentTerms  string         `gorm:"column:payment_terms;size:100" json:"payment_terms,omitempty"`
	BankAccountNo string         `gorm:"column:bank_account_no;size:50" json:"bank_account_no,omitempty"`
	BankIFSC      string         `gorm:"column:bank_ifsc;size:20" json:"bank_ifsc,omitempty"`
	BankName      string         `gorm:"column:bank_name;size:100" json:"bank_name,omitempty"`
	IsActive      bool           `gorm:"column:is_active;default:true" json:"is_active"`
	CreatedBy     uuid.UUID      `gorm:"column:created_by" json:"created_by"`
	CreatedAt     time.Time      `gorm:"column:created_at" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt     *time.Time     `gorm:"column:deleted_at;index" json:"deleted_at,omitempty"`
}

// TableName returns the table name for Vendor
func (Vendor) TableName() string {
	return "vendors"
}

// IsValidVendorType checks if a vendor type is valid
func IsValidVendorType(vt VendorType) bool {
	switch vt {
	case VendorTypeManufacturer, VendorTypeDistributor, VendorTypeWholesaler, VendorTypeRetailer, VendorTypeService, VendorTypeOther:
		return true
	default:
		return false
	}
}

// ValidateGSTIN validates Indian GSTIN format
func (v *Vendor) ValidateGSTIN() bool {
	if v.GSTIN == "" {
		return true // GSTIN is optional
	}
	pattern := `^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z]{1}[A-Z0-9]{1}[Z]{1}[A-Z0-9]{1}$`
	matched, _ := regexp.MatchString(pattern, v.GSTIN)
	return matched
}

// ValidatePAN validates Indian PAN format
func (v *Vendor) ValidatePAN() bool {
	if v.PAN == "" {
		return true // PAN is optional
	}
	pattern := `^[A-Z]{5}[0-9]{4}[A-Z]{1}$`
	matched, _ := regexp.MatchString(pattern, v.PAN)
	return matched
}

// IsOverCreditLimit checks if vendor balance exceeds credit limit
func (v *Vendor) IsOverCreditLimit() bool {
	return v.Balance.Abs().GreaterThan(v.CreditLimit)
}

// GetOutstandingBalance returns the absolute outstanding balance
func (v *Vendor) GetOutstandingBalance() decimal.Decimal {
	return v.Balance.Abs()
}

// VendorBalanceTransaction represents a balance change transaction for vendors
type VendorBalanceTransaction struct {
	ID              uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	VendorID        uuid.UUID      `gorm:"column:vendor_id;index:idx_vendor_balance_transactions_vendor_id;not null" json:"vendor_id"`
	Amount          decimal.Decimal `gorm:"type:decimal(15,2);not null" json:"amount"`
	BalanceBefore   decimal.Decimal `gorm:"type:decimal(15,2);not null" json:"balance_before"`
	BalanceAfter    decimal.Decimal `gorm:"type:decimal(15,2);not null" json:"balance_after"`
	TransactionType string         `gorm:"column:transaction_type;size:50;not null" json:"transaction_type"`
	Description     string         `gorm:"type:text" json:"description,omitempty"`
	ReferenceID     *uuid.UUID     `gorm:"column:reference_id" json:"reference_id,omitempty"`
	ReferenceType   string         `gorm:"column:reference_type;size:50" json:"reference_type,omitempty"`
	CreatedAt       time.Time      `gorm:"column:created_at" json:"created_at"`
}

// TableName returns the table name for VendorBalanceTransaction
func (VendorBalanceTransaction) TableName() string {
	return "vendor_balance_transactions"
}

// IsValidVendorTransactionType checks if a vendor transaction type is valid
func IsValidVendorTransactionType(tt string) bool {
	switch tt {
	case "credit", "debit", "payment_made", "purchase_created", "adjustment":
		return true
	default:
		return false
	}
}
