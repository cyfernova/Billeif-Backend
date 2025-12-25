package models

import (
	"regexp"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// BusinessType represents the type of business entity
type BusinessType string

const (
	BusinessTypeSoleProprietorship BusinessType = "sole_proprietorship"
	BusinessTypePartnership        BusinessType = "partnership"
	BusinessTypeLLP                BusinessType = "llp"
	BusinessTypePvtLtd             BusinessType = "pvt_ltd"
	BusinessTypePublicLtd          BusinessType = "public_ltd"
	BusinessTypeOther              BusinessType = "other"
)

// BusinessProfile represents a business entity in the system
type BusinessProfile struct {
	ID                   uuid.UUID    `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	UserID               uuid.UUID    `gorm:"column:user_id;index:idx_business_profiles_user_id;not null" json:"user_id"`
	Name                 string       `gorm:"size:255;not null" json:"name"`
	Type                 BusinessType `gorm:"size:50;not null" json:"type"`
	Address              string       `gorm:"type:text" json:"address,omitempty"`
	City                 string       `gorm:"size:100" json:"city,omitempty"`
	State                string       `gorm:"size:100" json:"state,omitempty"`
	StateCode            string       `gorm:"column:state_code;size:5" json:"state_code,omitempty"`
	Pincode              string       `gorm:"size:20" json:"pincode,omitempty"`
	GSTIN                string       `gorm:"size:15;uniqueIndex:idx_business_profiles_gstin" json:"gstin,omitempty"`
	PAN                  string       `gorm:"size:10" json:"pan,omitempty"`
	Email                string       `gorm:"size:255" json:"email,omitempty"`
	Phone                string       `gorm:"size:20" json:"phone,omitempty"`
	Website              string       `gorm:"size:255" json:"website,omitempty"`
	LogoURL              string       `gorm:"column:logo_url;size:500" json:"logo_url,omitempty"`
	InvoicePrefix        string       `gorm:"column:invoice_prefix;size:20;default:'INV'" json:"invoice_prefix"`
	InvoiceStartingNo    int          `gorm:"column:invoice_starting_no;default:1" json:"invoice_starting_no"`
	GSTReturnFrequency   string       `gorm:"column:gst_return_frequency;size:50;default:'monthly'" json:"gst_return_frequency"`
	BankName             string       `gorm:"column:bank_name;size:100" json:"bank_name,omitempty"`
	BankAccountNo        string       `gorm:"column:bank_account_no;size:50" json:"bank_account_no,omitempty"`
	BankIFSC             string       `gorm:"column:bank_ifsc;size:20" json:"bank_ifsc,omitempty"`
	BankBranch           string       `gorm:"column:bank_branch;size:100" json:"bank_branch,omitempty"`
	UPIID                string       `gorm:"column:upi_id;size:50" json:"upi_id,omitempty"`
	TermsAndConditions   string       `gorm:"column:terms_and_conditions;type:text" json:"terms_and_conditions,omitempty"`
	InvoiceNotes         string       `gorm:"column:invoice_notes;type:text" json:"invoice_notes,omitempty"`
	IsDefault            bool         `gorm:"column:is_default;default:false" json:"is_default"`
	CreatedAt            time.Time    `gorm:"column:created_at" json:"created_at"`
	UpdatedAt            time.Time    `gorm:"column:updated_at" json:"updated_at"`
	DeletedAt            *time.Time   `gorm:"column:deleted_at;index" json:"deleted_at,omitempty"`
}

// TableName returns the table name for BusinessProfile
func (BusinessProfile) TableName() string {
	return "business_profiles"
}

// BeforeCreate hook to ensure only one default per user
func (bp *BusinessProfile) BeforeCreate(tx *gorm.DB) error {
	if bp.IsDefault {
		return tx.Model(&BusinessProfile{}).
			Where("user_id = ? AND is_default = ?", bp.UserID, true).
			Update("is_default", false).Error
	}
	return nil
}

// IsValidBusinessType checks if a business type is valid
func IsValidBusinessType(bt BusinessType) bool {
	switch bt {
	case BusinessTypeSoleProprietorship, BusinessTypePartnership, BusinessTypeLLP, BusinessTypePvtLtd, BusinessTypePublicLtd, BusinessTypeOther:
		return true
	default:
		return false
	}
}

// ValidateGSTIN validates Indian GSTIN format
func (bp *BusinessProfile) ValidateGSTIN() bool {
	if bp.GSTIN == "" {
		return true // GSTIN is optional
	}
	pattern := `^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z]{1}[A-Z0-9]{1}[Z]{1}[A-Z0-9]{1}$`
	matched, _ := regexp.MatchString(pattern, bp.GSTIN)
	return matched
}

// ValidatePAN validates Indian PAN format
func (bp *BusinessProfile) ValidatePAN() bool {
	if bp.PAN == "" {
		return true // PAN is optional
	}
	pattern := `^[A-Z]{5}[0-9]{4}[A-Z]{1}$`
	matched, _ := regexp.MatchString(pattern, bp.PAN)
	return matched
}

// ValidateIFSC validates Indian IFSC code format (4 letters + 7 alphanumeric)
func (bp *BusinessProfile) ValidateIFSC() bool {
	if bp.BankIFSC == "" {
		return true // IFSC is optional
	}
	pattern := `^[A-Z]{4}[A-Z0-9]{7}$`
	matched, _ := regexp.MatchString(pattern, bp.BankIFSC)
	return matched
}

// ValidateUPIID validates UPI ID format
func (bp *BusinessProfile) ValidateUPIID() bool {
	if bp.UPIID == "" {
		return true // UPI ID is optional
	}
	pattern := `^[a-zA-Z0-9.\-_]{2,256}@[a-zA-Z]{2,64}$`
	matched, _ := regexp.MatchString(pattern, bp.UPIID)
	return matched
}

// GetNextInvoiceNumber generates the next invoice number
func (bp *BusinessProfile) GetNextInvoiceNumber() string {
	return bp.InvoicePrefix + padNumber(bp.InvoiceStartingNo, 6)
}

// SetAsDefault sets this profile as default and unsets others
func (bp *BusinessProfile) SetAsDefault(tx *gorm.DB) error {
	bp.IsDefault = true
	return tx.Model(&BusinessProfile{}).
		Where("user_id = ? AND id != ?", bp.UserID, bp.ID).
		Update("is_default", false).Error
}

// GetInvoiceSequence returns the current invoice sequence number
func (bp *BusinessProfile) GetInvoiceSequence() int {
	return bp.InvoiceStartingNo
}

// IncrementInvoiceSequence increments the invoice sequence number
func (bp *BusinessProfile) IncrementInvoiceSequence() {
	bp.InvoiceStartingNo++
}

// IsValidGSTFrequency checks if GST return frequency is valid
func IsValidGSTFrequency(freq string) bool {
	switch freq {
	case "monthly", "quarterly", "annually":
		return true
	default:
		return false
	}
}

// padNumber pads a number with leading zeros
func padNumber(num, width int) string {
	s := ""
	for i := 0; i < width-len(s); i++ {
		s += "0"
	}
	return s + string(rune(num))
}
