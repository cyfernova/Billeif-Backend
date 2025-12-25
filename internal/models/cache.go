package models

import "time"

// CustomerCache represents cached customer data in DynamoDB
type CustomerCache struct {
	BusinessID  string    `dynamodbav:"business_id" json:"business_id"`   // Partition Key
	CustomerID  string    `dynamodbav:"customer_id" json:"customer_id"`   // Sort Key
	Name        string    `dynamodbav:"name" json:"name"`
	Type        string    `dynamodbav:"customer_type" json:"customer_type"`
	Phone       string    `dynamodbav:"phone" json:"phone"`
	Email       string    `dynamodbav:"email" json:"email"`
	Address     string    `dynamodbav:"address" json:"address"`
	City        string    `dynamodbav:"city" json:"city"`
	State       string    `dynamodbav:"state" json:"state"`
	Pincode     string    `dynamodbav:"pincode" json:"pincode"`
	GSTIN       string    `dynamodbav:"gstin" json:"gstin"`
	PAN         string    `dynamodbav:"pan" json:"pan"`
	CreditLimit float64   `dynamodbav:"credit_limit" json:"credit_limit"`
	CreditPeriod int      `dynamodbav:"credit_period" json:"credit_period"`
	Balance     float64   `dynamodbav:"balance" json:"balance"`
	IsActive    bool      `dynamodbav:"is_active" json:"is_active"`
	CreatedBy   string    `dynamodbav:"created_by" json:"created_by"`
	CreatedAt   time.Time `dynamodbav:"created_at" json:"created_at"`
	CachedAt    time.Time `dynamodbav:"cached_at" json:"cached_at"`
	ExpiresAt   int64     `dynamodbav:"expires_at" json:"expires_at"` // TTL (Unix timestamp)
}

// GetTTL returns the TTL as a Unix timestamp
func (c *CustomerCache) GetTTL() int64 {
	return c.ExpiresAt
}

// SetTTL sets the TTL to 24 hours from now
func (c *CustomerCache) SetTTL(duration time.Duration) {
	c.ExpiresAt = time.Now().Add(duration).Unix()
}

// VendorCache represents cached vendor data in DynamoDB
type VendorCache struct {
	BusinessID    string    `dynamodbav:"business_id" json:"business_id"`   // Partition Key
	VendorID      string    `dynamodbav:"vendor_id" json:"vendor_id"`       // Sort Key
	Name          string    `dynamodbav:"name" json:"name"`
	Type          string    `dynamodbav:"vendor_type" json:"vendor_type"`
	Phone         string    `dynamodbav:"phone" json:"phone"`
	Email         string    `dynamodbav:"email" json:"email"`
	Address       string    `dynamodbav:"address" json:"address"`
	City          string    `dynamodbav:"city" json:"city"`
	State         string    `dynamodbav:"state" json:"state"`
	Pincode       string    `dynamodbav:"pincode" json:"pincode"`
	GSTIN         string    `dynamodbav:"gstin" json:"gstin"`
	PAN           string    `dynamodbav:"pan" json:"pan"`
	CreditLimit   float64   `dynamodbav:"credit_limit" json:"credit_limit"`
	CreditPeriod  int       `dynamodbav:"credit_period" json:"credit_period"`
	Balance       float64   `dynamodbav:"balance" json:"balance"`
	PaymentTerms  string    `dynamodbav:"payment_terms" json:"payment_terms"`
	BankAccountNo string    `dynamodbav:"bank_account_no" json:"bank_account_no"`
	BankIFSC      string    `dynamodbav:"bank_ifsc" json:"bank_ifsc"`
	BankName      string    `dynamodbav:"bank_name" json:"bank_name"`
	IsActive      bool      `dynamodbav:"is_active" json:"is_active"`
	CreatedBy     string    `dynamodbav:"created_by" json:"created_by"`
	CreatedAt     time.Time `dynamodbav:"created_at" json:"created_at"`
	CachedAt      time.Time `dynamodbav:"cached_at" json:"cached_at"`
	ExpiresAt     int64     `dynamodbav:"expires_at" json:"expires_at"` // TTL (Unix timestamp)
}

// GetTTL returns the TTL as a Unix timestamp
func (v *VendorCache) GetTTL() int64 {
	return v.ExpiresAt
}

// SetTTL sets the TTL to 24 hours from now
func (v *VendorCache) SetTTL(duration time.Duration) {
	v.ExpiresAt = time.Now().Add(duration).Unix()
}

// BusinessProfileCache represents cached business profile in DynamoDB
type BusinessProfileCache struct {
	UserID              string    `dynamodbav:"user_id" json:"user_id"`         // Partition Key
	ProfileID           string    `dynamodbav:"profile_id" json:"profile_id"`   // Sort Key
	Name                string    `dynamodbav:"name" json:"name"`
	Type                string    `dynamodbav:"business_type" json:"business_type"`
	Address             string    `dynamodbav:"address" json:"address"`
	City                string    `dynamodbav:"city" json:"city"`
	State               string    `dynamodbav:"state" json:"state"`
	StateCode           string    `dynamodbav:"state_code" json:"state_code"`
	Pincode             string    `dynamodbav:"pincode" json:"pincode"`
	GSTIN               string    `dynamodbav:"gstin" json:"gstin"`
	PAN                 string    `dynamodbav:"pan" json:"pan"`
	Email               string    `dynamodbav:"email" json:"email"`
	Phone               string    `dynamodbav:"phone" json:"phone"`
	Website             string    `dynamodbav:"website" json:"website"`
	LogoURL             string    `dynamodbav:"logo_url" json:"logo_url"`
	InvoicePrefix       string    `dynamodbav:"invoice_prefix" json:"invoice_prefix"`
	InvoiceStartingNo   int       `dynamodbav:"invoice_starting_no" json:"invoice_starting_no"`
	GSTReturnFrequency  string    `dynamodbav:"gst_return_frequency" json:"gst_return_frequency"`
	IsDefault           bool      `dynamodbav:"is_default" json:"is_default"`
	CreatedAt           time.Time `dynamodbav:"created_at" json:"created_at"`
	CachedAt            time.Time `dynamodbav:"cached_at" json:"cached_at"`
	ExpiresAt           int64     `dynamodbav:"expires_at" json:"expires_at"` // TTL (Unix timestamp)
}

// GetTTL returns the TTL as a Unix timestamp
func (b *BusinessProfileCache) GetTTL() int64 {
	return b.ExpiresAt
}

// SetTTL sets the TTL to 24 hours from now
func (b *BusinessProfileCache) SetTTL(duration time.Duration) {
	b.ExpiresAt = time.Now().Add(duration).Unix()
}

// Cache constants
const (
	// DefaultTTL is the default cache expiration time (24 hours)
	DefaultTTL = 24 * time.Hour

	// ShortTTL is for frequently changing data (1 hour)
	ShortTTL = 1 * time.Hour

	// LongTTL is for rarely changing data (7 days)
	LongTTL = 7 * 24 * time.Hour
)
