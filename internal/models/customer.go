package models

import (
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// CustomerType represents the type of customer
type CustomerType string

const (
	CustomerTypeRetail     CustomerType = "retail"
	CustomerTypeWholesale  CustomerType = "wholesale"
	CustomerTypeB2B        CustomerType = "b2b"
	CustomerTypeGovernment CustomerType = "government"
	CustomerTypeOther      CustomerType = "other"
)

// Customer represents a customer in the system
type Customer struct {
	ID            uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	BusinessID    uuid.UUID      `gorm:"column:business_id;index:idx_customers_business_id;not null" json:"business_id"`
	Name          string         `gorm:"size:255;not null;index:idx_customers_name" json:"name"`
	Type          CustomerType   `gorm:"size:50;not null;default:'retail'" json:"type"`
	Phone         string         `gorm:"size:20" json:"phone,omitempty"`
	Email         string         `gorm:"size:255" json:"email,omitempty"`
	Address       string         `gorm:"type:text" json:"address,omitempty"`
	City          string         `gorm:"size:100" json:"city,omitempty"`
	State         string         `gorm:"size:100" json:"state,omitempty"`
	Pincode       string         `gorm:"size:20" json:"pincode,omitempty"`
	GSTIN         string         `gorm:"column:gstin;size:15;uniqueIndex:idx_customers_gstin" json:"gstin,omitempty"`
	PAN           string         `gorm:"column:pan;size:10" json:"pan,omitempty"`
	CreditLimit   decimal.Decimal `gorm:"type:decimal(15,2);default:0" json:"credit_limit"`
	CreditPeriod  int            `gorm:"default:0" json:"credit_period"` // days
	Balance       decimal.Decimal `gorm:"type:decimal(15,2);default:0" json:"balance"`
	IsActive      bool           `gorm:"column:is_active;default:true" json:"is_active"`
	CreatedBy     uuid.UUID      `gorm:"column:created_by" json:"created_by"`
	CreatedAt     time.Time      `gorm:"column:created_at" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt     *time.Time     `gorm:"column:deleted_at;index" json:"deleted_at,omitempty"`
}

// TableName returns the table name for Customer
func (Customer) TableName() string {
	return "customers"
}

// IsValidCustomerType checks if a customer type is valid
func IsValidCustomerType(ct CustomerType) bool {
	switch ct {
	case CustomerTypeRetail, CustomerTypeWholesale, CustomerTypeB2B, CustomerTypeGovernment, CustomerTypeOther:
		return true
	default:
		return false
	}
}

// ValidateGSTIN validates Indian GSTIN format (2 characters + 10 digits + 1 character + 1 alphanumeric)
func (c *Customer) ValidateGSTIN() bool {
	if c.GSTIN == "" {
		return true // GSTIN is optional
	}
	// GSTIN is 15 characters: 2 state code + 10 PAN + 1 entity number + 1 alphabet + 1 check digit
	pattern := `^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z]{1}[A-Z0-9]{1}[Z]{1}[A-Z0-9]{1}$`
	matched, _ := regexp.MatchString(pattern, c.GSTIN)
	return matched
}

// ValidatePAN validates Indian PAN format (5 letters + 4 digits + 1 letter)
func (c *Customer) ValidatePAN() bool {
	if c.PAN == "" {
		return true // PAN is optional
	}
	pattern := `^[A-Z]{5}[0-9]{4}[A-Z]{1}$`
	matched, _ := regexp.MatchString(pattern, c.PAN)
	return matched
}

// IsOverCreditLimit checks if customer balance exceeds credit limit
func (c *Customer) IsOverCreditLimit() bool {
	return c.Balance.Abs().GreaterThan(c.CreditLimit)
}

// GetOutstandingBalance returns the absolute outstanding balance
func (c *Customer) GetOutstandingBalance() decimal.Decimal {
	return c.Balance.Abs()
}

// CustomerBalanceTransaction represents a balance change transaction
type CustomerBalanceTransaction struct {
	ID              uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	CustomerID      uuid.UUID      `gorm:"column:customer_id;index:idx_customer_balance_transactions_customer_id;not null" json:"customer_id"`
	Amount          decimal.Decimal `gorm:"type:decimal(15,2);not null" json:"amount"`
	BalanceBefore   decimal.Decimal `gorm:"type:decimal(15,2);not null" json:"balance_before"`
	BalanceAfter    decimal.Decimal `gorm:"type:decimal(15,2);not null" json:"balance_after"`
	TransactionType string         `gorm:"column:transaction_type;size:50;not null" json:"transaction_type"`
	Description     string         `gorm:"type:text" json:"description,omitempty"`
	ReferenceID     *uuid.UUID     `gorm:"column:reference_id" json:"reference_id,omitempty"`
	ReferenceType   string         `gorm:"column:reference_type;size:50" json:"reference_type,omitempty"`
	CreatedAt       time.Time      `gorm:"column:created_at;index:idx_customer_balance_transactions_created_at" json:"created_at"`
}

// TableName returns the table name for CustomerBalanceTransaction
func (CustomerBalanceTransaction) TableName() string {
	return "customer_balance_transactions"
}

// IsValidTransactionType checks if a transaction type is valid
func IsValidTransactionType(tt string) bool {
	switch tt {
	case "credit", "debit", "payment_received", "invoice_created", "adjustment":
		return true
	default:
		return false
	}
}
