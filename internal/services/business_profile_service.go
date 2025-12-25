package services

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/cyfernova/invoice-backend/internal/models"
	"github.com/cyfernova/invoice-backend/internal/repositories"
	"github.com/cyfernova/invoice-backend/internal/utils"
	"github.com/cyfernova/invoice-backend/pkg/awsclients"
	"github.com/google/uuid"
)

// BusinessProfileService handles business profile business logic
type BusinessProfileService struct {
	profileRepo repositories.BusinessProfileRepository
	s3Client    *awsclients.S3Client
	bucket      string
}

// NewBusinessProfileService creates a new business profile service
func NewBusinessProfileService(
	profileRepo repositories.BusinessProfileRepository,
	s3Client *awsclients.S3Client,
	bucket string,
) *BusinessProfileService {
	return &BusinessProfileService{
		profileRepo: profileRepo,
		s3Client:    s3Client,
		bucket:      bucket,
	}
}

// CreateBusinessProfileRequest represents business profile creation request
type CreateBusinessProfileRequest struct {
	Name                 string                `json:"name" validate:"required,min=2,max=255"`
	Type                 models.BusinessType   `json:"type" validate:"required"`
	Address              string                `json:"address" validate:"omitempty,max=500"`
	City                 string                `json:"city" validate:"omitempty,max=100"`
	State                string                `json:"state" validate:"omitempty,max=100"`
	StateCode            string                `json:"state_code" validate:"omitempty,max=5"`
	Pincode              string                `json:"pincode" validate:"omitempty,max=20"`
	GSTIN                string                `json:"gstin" validate:"omitempty,len=15"`
	PAN                  string                `json:"pan" validate:"omitempty,len=10"`
	Email                string                `json:"email" validate:"omitempty,email,max=255"`
	Phone                string                `json:"phone" validate:"omitempty,max=20"`
	Website              string                `json:"website" validate:"omitempty,url,max=255"`
	InvoicePrefix        string                `json:"invoice_prefix" validate:"omitempty,max=20,alpha"`
	InvoiceStartingNo    int                   `json:"invoice_starting_no" validate:"min=1"`
	GSTReturnFrequency   string                `json:"gst_return_frequency" validate:"omitempty,oneof=monthly quarterly annually"`
	BankName             string                `json:"bank_name" validate:"omitempty,max=100"`
	BankAccountNo        string                `json:"bank_account_no" validate:"omitempty,max=50"`
	BankIFSC             string                `json:"bank_ifsc" validate:"omitempty,max=20"`
	BankBranch           string                `json:"bank_branch" validate:"omitempty,max=100"`
	UPIID                string                `json:"upi_id" validate:"omitempty,max=50"`
	TermsAndConditions   string                `json:"terms_and_conditions"`
	InvoiceNotes         string                `json:"invoice_notes"`
	IsDefault            bool                  `json:"is_default"`
}

// UpdateBusinessProfileRequest represents business profile update request
type UpdateBusinessProfileRequest struct {
	Name                 *string              `json:"name" validate:"omitempty,min=2,max=255"`
	Type                 *models.BusinessType `json:"type" validate:"omitempty"`
	Address              *string              `json:"address" validate:"omitempty,max=500"`
	City                 *string              `json:"city" validate:"omitempty,max=100"`
	State                *string              `json:"state" validate:"omitempty,max=100"`
	StateCode            *string              `json:"state_code" validate:"omitempty,max=5"`
	Pincode              *string              `json:"pincode" validate:"omitempty,max=20"`
	GSTIN                *string              `json:"gstin" validate:"omitempty,len=15"`
	PAN                  *string              `json:"pan" validate:"omitempty,len=10"`
	Email                *string              `json:"email" validate:"omitempty,email,max=255"`
	Phone                *string              `json:"phone" validate:"omitempty,max=20"`
	Website              *string              `json:"website" validate:"omitempty,url,max=255"`
	LogoURL              *string              `json:"logo_url"`
	InvoicePrefix        *string              `json:"invoice_prefix" validate:"omitempty,max=20,alpha"`
	InvoiceStartingNo    *int                 `json:"invoice_starting_no" validate:"omitempty,min=1"`
	GSTReturnFrequency   *string              `json:"gst_return_frequency" validate:"omitempty,oneof=monthly quarterly annually"`
	BankName             *string              `json:"bank_name" validate:"omitempty,max=100"`
	BankAccountNo        *string              `json:"bank_account_no" validate:"omitempty,max=50"`
	BankIFSC             *string              `json:"bank_ifsc" validate:"omitempty,max=20"`
	BankBranch           *string              `json:"bank_branch" validate:"omitempty,max=100"`
	UPIID                *string              `json:"upi_id" validate:"omitempty,max=50"`
	TermsAndConditions   *string              `json:"terms_and_conditions"`
	InvoiceNotes         *string              `json:"invoice_notes"`
	IsDefault            *bool                `json:"is_default"`
}

// BusinessProfileResponse represents business profile response
type BusinessProfileResponse struct {
	ID                   string              `json:"id"`
	UserID               string              `json:"user_id"`
	Name                 string              `json:"name"`
	Type                 models.BusinessType `json:"type"`
	Address              string              `json:"address"`
	City                 string              `json:"city"`
	State                string              `json:"state"`
	StateCode            string              `json:"state_code"`
	Pincode              string              `json:"pincode"`
	GSTIN                string              `json:"gstin"`
	PAN                  string              `json:"pan"`
	Email                string              `json:"email"`
	Phone                string              `json:"phone"`
	Website              string              `json:"website"`
	LogoURL              string              `json:"logo_url"`
	InvoicePrefix        string              `json:"invoice_prefix"`
	InvoiceStartingNo    int                 `json:"invoice_starting_no"`
	GSTReturnFrequency   string              `json:"gst_return_frequency"`
	BankName             string              `json:"bank_name"`
	BankAccountNo        string              `json:"bank_account_no"`
	BankIFSC             string              `json:"bank_ifsc"`
	BankBranch           string              `json:"bank_branch"`
	UPIID                string              `json:"upi_id"`
	TermsAndConditions   string              `json:"terms_and_conditions"`
	InvoiceNotes         string              `json:"invoice_notes"`
	IsDefault            bool                `json:"is_default"`
	CreatedAt            time.Time           `json:"created_at"`
	UpdatedAt            time.Time           `json:"updated_at"`
}

// LogoUploadResponse represents logo upload response
type LogoUploadResponse struct {
	UploadURL    string `json:"upload_url"`
	Key          string `json:"key"`
	ExpiresAt    int64  `json:"expires_at"`
}

// Create creates a new business profile
func (s *BusinessProfileService) Create(ctx context.Context, userID string, req *CreateBusinessProfileRequest) (*BusinessProfileResponse, error) {
	parsedUserID, err := uuid.Parse(userID)
	if err != nil {
		return nil, utils.NewAppError(400, "Invalid user ID", err)
	}

	if !models.IsValidBusinessType(req.Type) {
		return nil, utils.NewAppError(400, "Invalid business type", nil)
	}

	// Check if GSTIN is unique
	if req.GSTIN != "" {
		existing, err := s.profileRepo.GetByGSTIN(ctx, req.GSTIN)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return nil, utils.NewAppError(409, "Business profile with this GSTIN already exists", nil)
		}
	}

	profile := &models.BusinessProfile{
		UserID:               parsedUserID,
		Name:                 req.Name,
		Type:                 req.Type,
		Address:              req.Address,
		City:                 req.City,
		State:                req.State,
		StateCode:            req.StateCode,
		Pincode:              req.Pincode,
		GSTIN:                req.GSTIN,
		PAN:                  req.PAN,
		Email:                req.Email,
		Phone:                req.Phone,
		Website:              req.Website,
		InvoicePrefix:        req.InvoicePrefix,
		InvoiceStartingNo:    req.InvoiceStartingNo,
		GSTReturnFrequency:   req.GSTReturnFrequency,
		BankName:             req.BankName,
		BankAccountNo:        req.BankAccountNo,
		BankIFSC:             req.BankIFSC,
		BankBranch:           req.BankBranch,
		UPIID:                req.UPIID,
		TermsAndConditions:   req.TermsAndConditions,
		InvoiceNotes:         req.InvoiceNotes,
		IsDefault:            req.IsDefault,
	}

	// Set defaults
	if profile.InvoicePrefix == "" {
		profile.InvoicePrefix = "INV"
	}
	if profile.GSTReturnFrequency == "" {
		profile.GSTReturnFrequency = "monthly"
	}

	// Validate
	if !profile.ValidateGSTIN() {
		return nil, utils.NewAppError(400, "Invalid GSTIN format", nil)
	}
	if !profile.ValidatePAN() {
		return nil, utils.NewAppError(400, "Invalid PAN format", nil)
	}

	if err := s.profileRepo.Create(ctx, profile); err != nil {
		return nil, err
	}

	return s.toResponse(profile), nil
}

// GetByID retrieves a business profile by ID
func (s *BusinessProfileService) GetByID(ctx context.Context, id string) (*BusinessProfileResponse, error) {
	profile, err := s.profileRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, utils.ErrNotFound
	}
	return s.toResponse(profile), nil
}

// GetByUserID retrieves all profiles for a user
func (s *BusinessProfileService) GetByUserID(ctx context.Context, userID string) ([]*BusinessProfileResponse, error) {
	profiles, err := s.profileRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	responses := make([]*BusinessProfileResponse, len(profiles))
	for i, p := range profiles {
		responses[i] = s.toResponse(p)
	}

	return responses, nil
}

// GetDefault retrieves the default profile for a user
func (s *BusinessProfileService) GetDefault(ctx context.Context, userID string) (*BusinessProfileResponse, error) {
	profile, err := s.profileRepo.GetDefault(ctx, userID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, utils.ErrNotFound
	}
	return s.toResponse(profile), nil
}

// Update updates an existing business profile
func (s *BusinessProfileService) Update(ctx context.Context, id string, req *UpdateBusinessProfileRequest) (*BusinessProfileResponse, error) {
	profile, err := s.profileRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, utils.ErrNotFound
	}

	if req.Name != nil {
		profile.Name = *req.Name
	}
	if req.Type != nil {
		if !models.IsValidBusinessType(*req.Type) {
			return nil, utils.NewAppError(400, "Invalid business type", nil)
		}
		profile.Type = *req.Type
	}
	if req.Address != nil {
		profile.Address = *req.Address
	}
	if req.City != nil {
		profile.City = *req.City
	}
	if req.State != nil {
		profile.State = *req.State
	}
	if req.StateCode != nil {
		profile.StateCode = *req.StateCode
	}
	if req.Pincode != nil {
		profile.Pincode = *req.Pincode
	}
	if req.GSTIN != nil {
		// Check uniqueness if GSTIN is changing
		if *req.GSTIN != profile.GSTIN {
			existing, err := s.profileRepo.GetByGSTIN(ctx, *req.GSTIN)
			if err == nil && existing != nil && existing.ID.String() != id {
				return nil, utils.NewAppError(409, "Business profile with this GSTIN already exists", nil)
			}
		}
		profile.GSTIN = *req.GSTIN
		if !profile.ValidateGSTIN() {
			return nil, utils.NewAppError(400, "Invalid GSTIN format", nil)
		}
	}
	if req.PAN != nil {
		profile.PAN = *req.PAN
		if !profile.ValidatePAN() {
			return nil, utils.NewAppError(400, "Invalid PAN format", nil)
		}
	}
	if req.Email != nil {
		profile.Email = *req.Email
	}
	if req.Phone != nil {
		profile.Phone = *req.Phone
	}
	if req.Website != nil {
		profile.Website = *req.Website
	}
	if req.LogoURL != nil {
		profile.LogoURL = *req.LogoURL
	}
	if req.InvoicePrefix != nil {
		profile.InvoicePrefix = *req.InvoicePrefix
	}
	if req.InvoiceStartingNo != nil {
		profile.InvoiceStartingNo = *req.InvoiceStartingNo
	}
	if req.GSTReturnFrequency != nil {
		if !models.IsValidGSTFrequency(*req.GSTReturnFrequency) {
			return nil, utils.NewAppError(400, "Invalid GST return frequency", nil)
		}
		profile.GSTReturnFrequency = *req.GSTReturnFrequency
	}
	if req.BankName != nil {
		profile.BankName = *req.BankName
	}
	if req.BankAccountNo != nil {
		profile.BankAccountNo = *req.BankAccountNo
	}
	if req.BankIFSC != nil {
		profile.BankIFSC = *req.BankIFSC
	}
	if req.BankBranch != nil {
		profile.BankBranch = *req.BankBranch
	}
	if req.UPIID != nil {
		profile.UPIID = *req.UPIID
	}
	if req.TermsAndConditions != nil {
		profile.TermsAndConditions = *req.TermsAndConditions
	}
	if req.InvoiceNotes != nil {
		profile.InvoiceNotes = *req.InvoiceNotes
	}
	if req.IsDefault != nil {
		profile.IsDefault = *req.IsDefault
		if profile.IsDefault {
			if err := s.profileRepo.SetDefault(ctx, id, profile.UserID.String()); err != nil {
				return nil, err
			}
		}
	}

	if err := s.profileRepo.Update(ctx, profile); err != nil {
		return nil, err
	}

	return s.toResponse(profile), nil
}

// Delete soft deletes a business profile
func (s *BusinessProfileService) Delete(ctx context.Context, id string) error {
	profile, err := s.profileRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if profile == nil {
		return utils.ErrNotFound
	}

	return s.profileRepo.Delete(ctx, id)
}

// HasAny checks if user has any business profiles
func (s *BusinessProfileService) HasAny(ctx context.Context, userID string) (bool, error) {
	return s.profileRepo.HasAny(ctx, userID)
}

// GetLogoUploadURL generates a pre-signed URL for logo upload
func (s *BusinessProfileService) GetLogoUploadURL(ctx context.Context, profileID string) (*LogoUploadResponse, error) {
	profile, err := s.profileRepo.GetByID(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, utils.ErrNotFound
	}

	// Generate unique key for the logo
	ext := "png"
	key := fmt.Sprintf("logos/%s/%s-%s.%s", profile.UserID.String(), profileID, time.Now().Format("20060102150405"), ext)

	// Generate pre-signed URL for upload (valid for 15 minutes)
	presignedResult, err := s.s3Client.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}

	return &LogoUploadResponse{
		UploadURL: presignedResult,
		Key:       key,
		ExpiresAt: time.Now().Add(15 * time.Minute).Unix(),
	}, nil
}

// ConfirmLogoUpload confirms logo upload and updates profile
func (s *BusinessProfileService) ConfirmLogoUpload(ctx context.Context, profileID, key string) (*BusinessProfileResponse, error) {
	// Generate public URL for the logo
	logoURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.bucket, "us-east-1", key)

	// Update profile
	if err := s.profileRepo.UpdateLogoURL(ctx, profileID, logoURL); err != nil {
		return nil, err
	}

	return s.GetByID(ctx, profileID)
}

// SetDefault sets a profile as default
func (s *BusinessProfileService) SetDefault(ctx context.Context, id string) (*BusinessProfileResponse, error) {
	profile, err := s.profileRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, utils.ErrNotFound
	}

	if err := s.profileRepo.SetDefault(ctx, id, profile.UserID.String()); err != nil {
		return nil, err
	}

	return s.GetByID(ctx, id)
}

// GetNextInvoiceNumber generates the next invoice number for a profile
func (s *BusinessProfileService) GetNextInvoiceNumber(ctx context.Context, profileID string) (string, int, error) {
	profile, err := s.profileRepo.GetByID(ctx, profileID)
	if err != nil {
		return "", 0, err
	}
	if profile == nil {
		return "", 0, utils.ErrNotFound
	}

	invoiceNumber := profile.GetNextInvoiceNumber()
	sequence := profile.GetInvoiceSequence()

	return invoiceNumber, sequence, nil
}

// IncrementInvoiceSequence increments and returns the new sequence number
func (s *BusinessProfileService) IncrementInvoiceSequence(ctx context.Context, profileID string) (int, error) {
	return s.profileRepo.IncrementInvoiceSequence(ctx, profileID)
}

// Search searches profiles by name
func (s *BusinessProfileService) Search(ctx context.Context, userID, name string) ([]*BusinessProfileResponse, error) {
	profiles, err := s.profileRepo.SearchByName(ctx, userID, name)
	if err != nil {
		return nil, err
	}

	responses := make([]*BusinessProfileResponse, len(profiles))
	for i, p := range profiles {
		responses[i] = s.toResponse(p)
	}

	return responses, nil
}

// GeneratePresignedDownloadURL generates a pre-signed URL for downloading files from S3
func (s *BusinessProfileService) GeneratePresignedDownloadURL(ctx context.Context, key string, expiration time.Duration) (string, error) {
	return s.s3Client.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
}

func (s *BusinessProfileService) toResponse(profile *models.BusinessProfile) *BusinessProfileResponse {
	return &BusinessProfileResponse{
		ID:                   profile.ID.String(),
		UserID:               profile.UserID.String(),
		Name:                 profile.Name,
		Type:                 profile.Type,
		Address:              profile.Address,
		City:                 profile.City,
		State:                profile.State,
		StateCode:            profile.StateCode,
		Pincode:              profile.Pincode,
		GSTIN:                profile.GSTIN,
		PAN:                  profile.PAN,
		Email:                profile.Email,
		Phone:                profile.Phone,
		Website:              profile.Website,
		LogoURL:              profile.LogoURL,
		InvoicePrefix:        profile.InvoicePrefix,
		InvoiceStartingNo:    profile.InvoiceStartingNo,
		GSTReturnFrequency:   profile.GSTReturnFrequency,
		BankName:             profile.BankName,
		BankAccountNo:        profile.BankAccountNo,
		BankIFSC:             profile.BankIFSC,
		BankBranch:           profile.BankBranch,
		UPIID:                profile.UPIID,
		TermsAndConditions:   profile.TermsAndConditions,
		InvoiceNotes:         profile.InvoiceNotes,
		IsDefault:            profile.IsDefault,
		CreatedAt:            profile.CreatedAt,
		UpdatedAt:            profile.UpdatedAt,
	}
}
