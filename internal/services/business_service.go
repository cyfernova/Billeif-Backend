package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type BusinessService struct {
	repo           interfaces.BusinessRepository
	s3             *S3Service
	log            *logger.Logger
	logoRepository interfaces.BusinessLogoRepository
	logoUploads    *PendingUploadService
	logoObjects    PendingObjectStore
	logoBucket     string
	now            func() time.Time
}

func NewBusinessService(repo interfaces.BusinessRepository, s3 *S3Service, log *logger.Logger) *BusinessService {
	return &BusinessService{repo: repo, s3: s3, log: log, now: time.Now}
}

var ErrBusinessLogoInvalid = errors.New("invalid business logo upload")

type BusinessLogoUploadInput struct {
	ContentType    string `json:"content_type" binding:"required"`
	SizeBytes      int64  `json:"size_bytes" binding:"required"`
	ChecksumSHA256 string `json:"checksum_sha256" binding:"required"`
}

type BusinessLogoCompletionInput struct {
	UploadID string `json:"upload_id" binding:"required,uuid"`
}

type BusinessLogoCompletion struct {
	BusinessID     string `json:"business_id"`
	UploadID       string `json:"upload_id"`
	Status         string `json:"status"`
	Replayed       bool   `json:"replayed"`
	CleanupPending bool   `json:"cleanup_pending"`
}

func (s *BusinessService) WithLogoUploadWorkflow(repository interfaces.BusinessLogoRepository, uploads *PendingUploadService, objects PendingObjectStore, bucket string) *BusinessService {
	if s != nil {
		s.logoRepository = repository
		s.logoUploads = uploads
		s.logoObjects = objects
		s.logoBucket = strings.TrimSpace(bucket)
	}
	return s
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
	PostalCode          string                 `json:"postal_code"`
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

func resolvedPostalCode(zipCode, postalCode string) string {
	if zipCode != "" {
		return zipCode
	}
	return postalCode
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
		PostalCode:          resolvedPostalCode(input.ZipCode, input.PostalCode),
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
	PostalCode          string                 `json:"postal_code"`
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
	if postalCode := resolvedPostalCode(input.ZipCode, input.PostalCode); postalCode != "" {
		business.PostalCode = postalCode
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
	if postalCode := resolvedPostalCode(input.ZipCode, input.PostalCode); postalCode != "" {
		business.PostalCode = postalCode
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

func (s *BusinessService) GetLogoUploadURL(ctx context.Context, businessID, contentType string, sizeBytes int64) (*PresignedUpload, error) {
	log := logger.FromContext(ctx).With("service", "business", "operation", "get_logo_upload_url", "business_id", businessID)
	if s.s3 == nil || s.s3.cfg == nil || strings.TrimSpace(s.s3.cfg.S3.BucketLogos) == "" {
		return nil, fmt.Errorf("business logo storage is not configured")
	}
	contentType, err := NormalizeImageUploadContentType(contentType)
	if err != nil {
		return nil, err
	}
	if err := validateUploadSize("business logo", sizeBytes, MaxBusinessLogoUploadBytes); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("logos/%s/logo", businessID)
	upload, err := s.s3.GeneratePresignedUpload(ctx, s.s3.cfg.S3.BucketLogos, key, contentType, sizeBytes, 3600)
	if err != nil {
		log.Error("failed to generate business logo upload URL", "error", err)
		return nil, err
	}
	log.Debug("generated business logo upload URL")
	return upload, nil
}

func (s *BusinessService) GetLogoUploadURLByOwner(ctx context.Context, userID, businessID, contentType string, sizeBytes int64) (*PresignedUpload, error) {
	if _, err := s.GetByOwner(ctx, userID, businessID); err != nil {
		return nil, err
	}
	return s.GetLogoUploadURL(ctx, businessID, contentType, sizeBytes)
}

func (s *BusinessService) CreateLogoUpload(ctx context.Context, uploaderID, businessID string, input BusinessLogoUploadInput) (*PendingUploadCreated, error) {
	if s == nil || s.logoUploads == nil || s.logoRepository == nil || s.logoObjects == nil || s.logoBucket == "" {
		return nil, fmt.Errorf("business logo storage is not configured")
	}
	if _, err := s.GetByOwner(ctx, strings.TrimSpace(uploaderID), strings.TrimSpace(businessID)); err != nil {
		return nil, err
	}
	contentType := strings.ToLower(strings.TrimSpace(input.ContentType))
	if !allowedBusinessLogoContentType(contentType) || validateUploadSize("business logo", input.SizeBytes, MaxBusinessLogoUploadBytes) != nil {
		return nil, ErrBusinessLogoInvalid
	}
	created, err := s.logoUploads.Create(ctx, PendingUploadCreateInput{
		BusinessID: strings.TrimSpace(businessID), UploaderID: strings.TrimSpace(uploaderID), Kind: "business_logo",
		ContentType: contentType, SizeBytes: input.SizeBytes, ChecksumSHA256: strings.TrimSpace(input.ChecksumSHA256),
	})
	if err != nil {
		if errors.Is(err, ErrPendingUploadInvalid) {
			return nil, ErrBusinessLogoInvalid
		}
		return nil, err
	}
	return created, nil
}

func (s *BusinessService) FinalizeLogoUpload(ctx context.Context, uploaderID, businessID, uploadID string) (*BusinessLogoCompletion, error) {
	if s == nil || s.logoRepository == nil || s.logoObjects == nil || s.logoBucket == "" {
		return nil, fmt.Errorf("business logo storage is not configured")
	}
	uploaderID, businessID, uploadID = strings.TrimSpace(uploaderID), strings.TrimSpace(businessID), strings.TrimSpace(uploadID)
	if _, err := s.GetByOwner(ctx, uploaderID, businessID); err != nil {
		return nil, err
	}
	upload, err := s.logoRepository.GetPendingUpload(ctx, uploadID, businessID, uploaderID)
	if err != nil || upload == nil {
		return nil, ErrPendingUploadNotFound
	}
	now := s.now().UTC()
	expectedKey := fmt.Sprintf("pending/%s/%s/business_logo", businessID, uploadID)
	attached := upload.ScanCode == models.PendingUploadScanBusinessLogo
	if upload.Kind != "business_logo" || upload.Bucket != s.logoBucket || upload.ObjectKey != expectedKey ||
		!allowedBusinessLogoContentType(upload.ContentType) || upload.SizeBytes <= 0 || upload.SizeBytes > MaxBusinessLogoUploadBytes ||
		!validChecksumSHA256(upload.ChecksumSHA256) || (!attached && !upload.ExpiresAt.After(now)) ||
		(upload.Status != models.PendingUploadStatusPending && upload.Status != models.PendingUploadStatusClean) {
		return nil, ErrBusinessLogoInvalid
	}
	if !attached {
		metadata, inspectErr := s.logoObjects.InspectPendingObject(ctx, upload.Bucket, upload.ObjectKey)
		if inspectErr != nil || !pendingMetadataMatches(upload, metadata) {
			return nil, ErrBusinessLogoInvalid
		}
	}
	finalized, err := s.logoRepository.FinalizeBusinessLogo(ctx, interfaces.BusinessLogoFinalizeRequest{
		BusinessID: businessID, UploaderID: uploaderID, UploadID: upload.ID, Bucket: upload.Bucket,
		ObjectKey: upload.ObjectKey, ContentType: upload.ContentType, SizeBytes: upload.SizeBytes,
		ChecksumSHA256: upload.ChecksumSHA256, CompletedAt: now,
	})
	if err != nil || finalized == nil || finalized.Business == nil {
		if err != nil {
			if errors.Is(err, interfaces.ErrBusinessLogoFinalizeConflict) {
				return nil, ErrBusinessLogoInvalid
			}
			return nil, fmt.Errorf("finalize business logo: %w", err)
		}
		return nil, ErrBusinessLogoInvalid
	}
	completion := &BusinessLogoCompletion{
		BusinessID: businessID, UploadID: upload.ID, Status: "ready", Replayed: finalized.Replayed,
	}
	if finalized.Business.LogoKey != upload.ObjectKey {
		completion.Status = "superseded"
	}
	oldKey := strings.TrimSpace(finalized.PreviousObjectKey)
	if oldKey != "" && oldKey != upload.ObjectKey {
		if !validBusinessLogoObjectKey(businessID, oldKey) || s.logoObjects.DeletePendingObject(ctx, s.logoBucket, oldKey) != nil {
			completion.CleanupPending = true
		} else if cleanupErr := s.logoRepository.CompleteBusinessLogoCleanup(ctx, upload.ID, businessID, uploaderID, oldKey); cleanupErr != nil {
			completion.CleanupPending = true
		}
	}
	return completion, nil
}

func allowedBusinessLogoContentType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func validBusinessLogoObjectKey(businessID, key string) bool {
	return strings.HasPrefix(key, "logos/"+businessID+"/") || strings.HasPrefix(key, "pending/"+businessID+"/")
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
	GeneratePresignedUpload(ctx context.Context, bucket, key, contentType string, sizeBytes, expiresIn int64) (*PresignedUpload, error)
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
		PostalCode: resolvedPostalCode(input.ZipCode, input.PostalCode),
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
	if postalCode := resolvedPostalCode(input.ZipCode, input.PostalCode); postalCode != "" {
		business.PostalCode = postalCode
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
	if postalCode := resolvedPostalCode(input.ZipCode, input.PostalCode); postalCode != "" {
		business.PostalCode = postalCode
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
func (s *BusinessServiceTestable) GetLogoUploadURL(ctx context.Context, businessID, contentType string, sizeBytes int64) (*PresignedUpload, error) {
	contentType, err := NormalizeImageUploadContentType(contentType)
	if err != nil {
		return nil, err
	}
	if err := validateUploadSize("business logo", sizeBytes, MaxBusinessLogoUploadBytes); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("logos/%s/logo", businessID)
	return s.s3.GeneratePresignedUpload(ctx, "business-logos", key, contentType, sizeBytes, 3600)
}

// GetLogoUploadURLByOwner generates a presigned URL for logo upload scoped to owner (testable version)
func (s *BusinessServiceTestable) GetLogoUploadURLByOwner(ctx context.Context, userID, businessID, contentType string, sizeBytes int64) (*PresignedUpload, error) {
	if _, err := s.GetByOwner(ctx, userID, businessID); err != nil {
		return nil, err
	}
	return s.GetLogoUploadURL(ctx, businessID, contentType, sizeBytes)
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
