package services

import (
	"context"
	"errors"
	"fmt"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type BusinessService struct {
	repo interfaces.BusinessRepository
	s3   *S3Service
	log  *logger.Logger
}

func NewBusinessService(repo interfaces.BusinessRepository, s3 *S3Service, log *logger.Logger) *BusinessService {
	return &BusinessService{repo: repo, s3: s3, log: log}
}

type CreateBusinessInput struct {
	Name                string                 `json:"name" binding:"required,min=2"`
	Email               string                 `json:"email" binding:"required,email"`
	Phone               string                 `json:"phone"`
	Address             string                 `json:"address"`
	City                string                 `json:"city"`
	State               string                 `json:"state"`
	Country             string                 `json:"country"`
	ZipCode             string                 `json:"zip_code"`
	TaxID               string                 `json:"tax_id"`
	GSTIN               string                 `json:"gstin"`
	BusinessStateCode   string                 `json:"business_state_code"`
	CompositionEnabled  bool                   `json:"composition_enabled"`
	DefaultGSTTreatment string                 `json:"default_gst_treatment"`
	GSTFilingFrequency  string                 `json:"gst_filing_frequency"`
	GSTRegistered       bool                   `json:"gst_registered"`
	GSTTDSEnabled       bool                   `json:"gst_tds_enabled"`
	ExportLUTEnabled    bool                   `json:"export_lut_enabled"`
	SEZEnabled          bool                   `json:"sez_enabled"`
	NumberingRules      map[string]interface{} `json:"numbering_rules,omitempty"`
	TaxPreferencesJSON  map[string]interface{} `json:"tax_preferences_json,omitempty"`
	Currency            string                 `json:"currency"`
	InvoicePrefix       string                 `json:"invoice_prefix"`
}

func (s *BusinessService) Create(ctx context.Context, userID string, input CreateBusinessInput) (*models.BusinessProfile, error) {
	log := logger.FromContext(ctx).With("service", "business", "operation", "create", "owner_id", userID)
	business := &models.BusinessProfile{
		OwnerID:             userID,
		Name:                input.Name,
		Email:               input.Email,
		Phone:               input.Phone,
		Address:             input.Address,
		City:                input.City,
		State:               input.State,
		Country:             input.Country,
		PostalCode:          input.ZipCode,
		TaxID:               input.TaxID,
		GSTIN:               input.GSTIN,
		BusinessStateCode:   input.BusinessStateCode,
		CompositionEnabled:  input.CompositionEnabled,
		DefaultGSTTreatment: input.DefaultGSTTreatment,
		GSTFilingFrequency:  input.GSTFilingFrequency,
		GSTRegistered:       input.GSTRegistered,
		GSTTDSEnabled:       input.GSTTDSEnabled,
		ExportLUTEnabled:    input.ExportLUTEnabled,
		SEZEnabled:          input.SEZEnabled,
		NumberingRules:      mustMarshalMap(input.NumberingRules),
		TaxPreferencesJSON:  mustMarshalMap(input.TaxPreferencesJSON),
		Currency:            input.Currency,
	}

	if business.Currency == "" {
		business.Currency = "USD"
	}
	if business.DefaultGSTTreatment == "" {
		business.DefaultGSTTreatment = models.DocumentGSTTreatmentRegular
	}
	if business.GSTFilingFrequency == "" {
		business.GSTFilingFrequency = "monthly"
	}

	if err := s.repo.Create(ctx, business); err != nil {
		log.Error("failed to create business", "error", err)
		return nil, fmt.Errorf("failed to create business: %w", err)
	}

	log.Info("business created", "business_id", business.ID)
	return business, nil
}

func (s *BusinessService) Get(ctx context.Context, id string) (*models.BusinessProfile, error) {
	log := logger.FromContext(ctx).With("service", "business", "operation", "get", "business_id", id)
	business, err := s.repo.GetByID(ctx, id)
	if err != nil {
		log.Error("failed to get business", "error", err)
		return nil, err
	}
	return business, nil
}

func (s *BusinessService) GetByOwner(ctx context.Context, userID, businessID string) (*models.BusinessProfile, error) {
	business, err := s.Get(ctx, businessID)
	if err != nil {
		return nil, err
	}
	if business.OwnerID != userID {
		return nil, errors.New("business not found")
	}
	return business, nil
}

func (s *BusinessService) List(ctx context.Context, userID string, page, limit int) ([]*models.BusinessProfile, int64, error) {
	log := logger.FromContext(ctx).With("service", "business", "operation", "list", "owner_id", userID, "page", page, "limit", limit)
	businesses, total, err := s.repo.List(ctx, userID, page, limit)
	if err != nil {
		log.Error("failed to list businesses", "error", err)
		return nil, 0, err
	}
	log.Debug("listed businesses", "count", len(businesses), "total", total)
	return businesses, total, nil
}

type UpdateBusinessInput struct {
	Name                string                 `json:"name"`
	Email               string                 `json:"email"`
	Phone               string                 `json:"phone"`
	Address             string                 `json:"address"`
	City                string                 `json:"city"`
	State               string                 `json:"state"`
	Country             string                 `json:"country"`
	ZipCode             string                 `json:"zip_code"`
	TaxID               string                 `json:"tax_id"`
	GSTIN               string                 `json:"gstin"`
	BusinessStateCode   string                 `json:"business_state_code"`
	CompositionEnabled  *bool                  `json:"composition_enabled"`
	DefaultGSTTreatment string                 `json:"default_gst_treatment"`
	GSTFilingFrequency  string                 `json:"gst_filing_frequency"`
	GSTRegistered       *bool                  `json:"gst_registered"`
	GSTTDSEnabled       *bool                  `json:"gst_tds_enabled"`
	ExportLUTEnabled    *bool                  `json:"export_lut_enabled"`
	SEZEnabled          *bool                  `json:"sez_enabled"`
	NumberingRules      map[string]interface{} `json:"numbering_rules,omitempty"`
	TaxPreferencesJSON  map[string]interface{} `json:"tax_preferences_json,omitempty"`
	Currency            string                 `json:"currency"`
	InvoicePrefix       string                 `json:"invoice_prefix"`
}

func (s *BusinessService) Update(ctx context.Context, id string, input UpdateBusinessInput) (*models.BusinessProfile, error) {
	log := logger.FromContext(ctx).With("service", "business", "operation", "update", "business_id", id)
	business, err := s.repo.GetByID(ctx, id)
	if err != nil {
		log.Error("failed to load business for update", "error", err)
		return nil, err
	}

	if input.Name != "" {
		business.Name = input.Name
	}
	if input.Email != "" {
		business.Email = input.Email
	}
	if input.Phone != "" {
		business.Phone = input.Phone
	}
	if input.Address != "" {
		business.Address = input.Address
	}
	if input.City != "" {
		business.City = input.City
	}
	if input.State != "" {
		business.State = input.State
	}
	if input.Country != "" {
		business.Country = input.Country
	}
	if input.ZipCode != "" {
		business.PostalCode = input.ZipCode
	}
	if input.TaxID != "" {
		business.TaxID = input.TaxID
	}
	if input.GSTIN != "" {
		business.GSTIN = input.GSTIN
	}
	if input.BusinessStateCode != "" {
		business.BusinessStateCode = input.BusinessStateCode
	}
	if input.CompositionEnabled != nil {
		business.CompositionEnabled = *input.CompositionEnabled
	}
	if input.DefaultGSTTreatment != "" {
		business.DefaultGSTTreatment = input.DefaultGSTTreatment
	}
	if input.GSTFilingFrequency != "" {
		business.GSTFilingFrequency = input.GSTFilingFrequency
	}
	if input.GSTRegistered != nil {
		business.GSTRegistered = *input.GSTRegistered
	}
	if input.GSTTDSEnabled != nil {
		business.GSTTDSEnabled = *input.GSTTDSEnabled
	}
	if input.ExportLUTEnabled != nil {
		business.ExportLUTEnabled = *input.ExportLUTEnabled
	}
	if input.SEZEnabled != nil {
		business.SEZEnabled = *input.SEZEnabled
	}
	if input.NumberingRules != nil {
		business.NumberingRules = mustMarshalMap(input.NumberingRules)
	}
	if input.TaxPreferencesJSON != nil {
		business.TaxPreferencesJSON = mustMarshalMap(input.TaxPreferencesJSON)
	}
	if input.Currency != "" {
		business.Currency = input.Currency
	}

	if err := s.repo.Update(ctx, business); err != nil {
		log.Error("failed to update business", "error", err)
		return nil, err
	}

	log.Info("business updated", "business_id", business.ID)
	return business, nil
}

func (s *BusinessService) UpdateByOwner(ctx context.Context, userID, id string, input UpdateBusinessInput) (*models.BusinessProfile, error) {
	log := logger.FromContext(ctx).With("service", "business", "operation", "update", "business_id", id, "owner_id", userID)
	business, err := s.GetByOwner(ctx, userID, id)
	if err != nil {
		log.Error("failed to load business for scoped update", "error", err)
		return nil, err
	}

	if input.Name != "" {
		business.Name = input.Name
	}
	if input.Email != "" {
		business.Email = input.Email
	}
	if input.Phone != "" {
		business.Phone = input.Phone
	}
	if input.Address != "" {
		business.Address = input.Address
	}
	if input.City != "" {
		business.City = input.City
	}
	if input.State != "" {
		business.State = input.State
	}
	if input.Country != "" {
		business.Country = input.Country
	}
	if input.ZipCode != "" {
		business.PostalCode = input.ZipCode
	}
	if input.TaxID != "" {
		business.TaxID = input.TaxID
	}
	if input.GSTIN != "" {
		business.GSTIN = input.GSTIN
	}
	if input.BusinessStateCode != "" {
		business.BusinessStateCode = input.BusinessStateCode
	}
	if input.CompositionEnabled != nil {
		business.CompositionEnabled = *input.CompositionEnabled
	}
	if input.DefaultGSTTreatment != "" {
		business.DefaultGSTTreatment = input.DefaultGSTTreatment
	}
	if input.GSTFilingFrequency != "" {
		business.GSTFilingFrequency = input.GSTFilingFrequency
	}
	if input.GSTRegistered != nil {
		business.GSTRegistered = *input.GSTRegistered
	}
	if input.GSTTDSEnabled != nil {
		business.GSTTDSEnabled = *input.GSTTDSEnabled
	}
	if input.ExportLUTEnabled != nil {
		business.ExportLUTEnabled = *input.ExportLUTEnabled
	}
	if input.SEZEnabled != nil {
		business.SEZEnabled = *input.SEZEnabled
	}
	if input.NumberingRules != nil {
		business.NumberingRules = mustMarshalMap(input.NumberingRules)
	}
	if input.TaxPreferencesJSON != nil {
		business.TaxPreferencesJSON = mustMarshalMap(input.TaxPreferencesJSON)
	}
	if input.Currency != "" {
		business.Currency = input.Currency
	}

	if err := s.repo.Update(ctx, business); err != nil {
		log.Error("failed to update business", "error", err)
		return nil, err
	}
	log.Info("business updated", "business_id", business.ID)
	return business, nil
}

func (s *BusinessService) Delete(ctx context.Context, id string) error {
	log := logger.FromContext(ctx).With("service", "business", "operation", "delete", "business_id", id)
	if err := s.repo.Delete(ctx, id); err != nil {
		log.Error("failed to delete business", "error", err)
		return err
	}
	log.Info("business deleted", "business_id", id)
	return nil
}

func (s *BusinessService) DeleteByOwner(ctx context.Context, userID, id string) error {
	log := logger.FromContext(ctx).With("service", "business", "operation", "delete", "business_id", id, "owner_id", userID)
	business, err := s.GetByOwner(ctx, userID, id)
	if err != nil {
		log.Error("failed to load business for scoped delete", "error", err)
		return err
	}
	if err := s.repo.Delete(ctx, business.ID); err != nil {
		log.Error("failed to delete business", "error", err)
		return err
	}
	log.Info("business deleted", "business_id", business.ID)
	return nil
}

func (s *BusinessService) GetLogoUploadURL(ctx context.Context, businessID, contentType string) (string, error) {
	log := logger.FromContext(ctx).With("service", "business", "operation", "get_logo_upload_url", "business_id", businessID)
	key := fmt.Sprintf("logos/%s/logo", businessID)
	url, err := s.s3.GeneratePresignedUploadURL(ctx, "business-logos", key, contentType, 3600)
	if err != nil {
		log.Error("failed to generate business logo upload URL", "error", err)
		return "", err
	}
	log.Debug("generated business logo upload URL")
	return url, nil
}

func (s *BusinessService) GetLogoUploadURLByOwner(ctx context.Context, userID, businessID, contentType string) (string, error) {
	if _, err := s.GetByOwner(ctx, userID, businessID); err != nil {
		return "", err
	}
	return s.GetLogoUploadURL(ctx, businessID, contentType)
}

func (s *BusinessService) UpdateLogoURL(ctx context.Context, businessID, logoURL string) error {
	log := logger.FromContext(ctx).With("service", "business", "operation", "update_logo_url", "business_id", businessID)
	business, err := s.repo.GetByID(ctx, businessID)
	if err != nil {
		log.Error("failed to load business for logo update", "error", err)
		return err
	}
	business.LogoURL = logoURL
	if err := s.repo.Update(ctx, business); err != nil {
		log.Error("failed to update business logo URL", "error", err)
		return err
	}
	log.Info("business logo URL updated")
	return nil
}

// BusinessServiceTestable is a test-friendly version of BusinessService
type BusinessServiceTestable struct {
	repo BusinessRepositoryTestable
	s3   BusinessS3ServiceTestable
	log  *logger.Logger
}

// BusinessS3ServiceTestable is the testable interface for S3 operations
type BusinessS3ServiceTestable interface {
	GeneratePresignedUploadURL(ctx context.Context, bucket, key, contentType string, expiresIn int64) (string, error)
}

// BusinessRepositoryTestable is the testable interface for BusinessRepository
type BusinessRepositoryTestable interface {
	Create(ctx context.Context, business *models.BusinessProfile) error
	GetByID(ctx context.Context, id string) (*models.BusinessProfile, error)
	Update(ctx context.Context, business *models.BusinessProfile) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, userID string, page, limit int) ([]*models.BusinessProfile, int64, error)
}

// NewBusinessServiceForTesting creates a BusinessServiceTestable for unit testing
func NewBusinessServiceForTesting(repo BusinessRepositoryTestable, s3 BusinessS3ServiceTestable, log *logger.Logger) *BusinessServiceTestable {
	return &BusinessServiceTestable{
		repo: repo,
		s3:   s3,
		log:  log,
	}
}

// Create creates a business (testable version)
func (s *BusinessServiceTestable) Create(ctx context.Context, userID string, input CreateBusinessInput) (*models.BusinessProfile, error) {
	business := &models.BusinessProfile{
		OwnerID:    userID,
		Name:       input.Name,
		Email:      input.Email,
		Phone:      input.Phone,
		Address:    input.Address,
		City:       input.City,
		State:      input.State,
		Country:    input.Country,
		PostalCode: input.ZipCode,
		TaxID:      input.TaxID,
		Currency:   input.Currency,
	}

	if business.Currency == "" {
		business.Currency = "USD"
	}

	if err := s.repo.Create(ctx, business); err != nil {
		return nil, fmt.Errorf("failed to create business: %w", err)
	}

	return business, nil
}

// Get retrieves a business by ID (testable version)
func (s *BusinessServiceTestable) Get(ctx context.Context, id string) (*models.BusinessProfile, error) {
	return s.repo.GetByID(ctx, id)
}

// GetByOwner retrieves a business by owner and business ID (testable version)
func (s *BusinessServiceTestable) GetByOwner(ctx context.Context, userID, businessID string) (*models.BusinessProfile, error) {
	business, err := s.Get(ctx, businessID)
	if err != nil {
		return nil, err
	}
	if business.OwnerID != userID {
		return nil, errors.New("business not found")
	}
	return business, nil
}

// List lists businesses by owner (testable version)
func (s *BusinessServiceTestable) List(ctx context.Context, userID string, page, limit int) ([]*models.BusinessProfile, int64, error) {
	return s.repo.List(ctx, userID, page, limit)
}

// Update updates a business (testable version)
func (s *BusinessServiceTestable) Update(ctx context.Context, id string, input UpdateBusinessInput) (*models.BusinessProfile, error) {
	business, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.Name != "" {
		business.Name = input.Name
	}
	if input.Email != "" {
		business.Email = input.Email
	}
	if input.Phone != "" {
		business.Phone = input.Phone
	}
	if input.Address != "" {
		business.Address = input.Address
	}
	if input.City != "" {
		business.City = input.City
	}
	if input.State != "" {
		business.State = input.State
	}
	if input.Country != "" {
		business.Country = input.Country
	}
	if input.ZipCode != "" {
		business.PostalCode = input.ZipCode
	}
	if input.TaxID != "" {
		business.TaxID = input.TaxID
	}
	if input.Currency != "" {
		business.Currency = input.Currency
	}

	if err := s.repo.Update(ctx, business); err != nil {
		return nil, err
	}

	return business, nil
}

// UpdateByOwner updates a business scoped to owner (testable version)
func (s *BusinessServiceTestable) UpdateByOwner(ctx context.Context, userID, id string, input UpdateBusinessInput) (*models.BusinessProfile, error) {
	business, err := s.GetByOwner(ctx, userID, id)
	if err != nil {
		return nil, err
	}

	if input.Name != "" {
		business.Name = input.Name
	}
	if input.Email != "" {
		business.Email = input.Email
	}
	if input.Phone != "" {
		business.Phone = input.Phone
	}
	if input.Address != "" {
		business.Address = input.Address
	}
	if input.City != "" {
		business.City = input.City
	}
	if input.State != "" {
		business.State = input.State
	}
	if input.Country != "" {
		business.Country = input.Country
	}
	if input.ZipCode != "" {
		business.PostalCode = input.ZipCode
	}
	if input.TaxID != "" {
		business.TaxID = input.TaxID
	}
	if input.Currency != "" {
		business.Currency = input.Currency
	}

	if err := s.repo.Update(ctx, business); err != nil {
		return nil, err
	}

	return business, nil
}

// Delete deletes a business (testable version)
func (s *BusinessServiceTestable) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

// DeleteByOwner deletes a business scoped to owner (testable version)
func (s *BusinessServiceTestable) DeleteByOwner(ctx context.Context, userID, id string) error {
	business, err := s.GetByOwner(ctx, userID, id)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, business.ID)
}

// GetLogoUploadURL generates a presigned URL for logo upload (testable version)
func (s *BusinessServiceTestable) GetLogoUploadURL(ctx context.Context, businessID, contentType string) (string, error) {
	key := fmt.Sprintf("logos/%s/logo", businessID)
	return s.s3.GeneratePresignedUploadURL(ctx, "business-logos", key, contentType, 3600)
}

// GetLogoUploadURLByOwner generates a presigned URL for logo upload scoped to owner (testable version)
func (s *BusinessServiceTestable) GetLogoUploadURLByOwner(ctx context.Context, userID, businessID, contentType string) (string, error) {
	if _, err := s.GetByOwner(ctx, userID, businessID); err != nil {
		return "", err
	}
	return s.GetLogoUploadURL(ctx, businessID, contentType)
}

// UpdateLogoURL updates the logo URL for a business (testable version)
func (s *BusinessServiceTestable) UpdateLogoURL(ctx context.Context, businessID, logoURL string) error {
	business, err := s.repo.GetByID(ctx, businessID)
	if err != nil {
		return err
	}
	business.LogoURL = logoURL
	return s.repo.Update(ctx, business)
}
