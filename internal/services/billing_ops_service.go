package services

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type BillingOpsService struct {
	cfg          *config.Config
	db           *gorm.DB
	customerRepo interfaces.CustomerRepository
	vendorRepo   interfaces.VendorRepository
	productRepo  interfaces.ProductRepository
	invoices     billingOpsInvoiceService
	documents    *DocumentService
	s3           *S3Service
	permissions  PermissionChecker
	log          *logger.Logger
}

type billingOpsInvoiceService interface {
	canonicalInvoiceCreator
	GetByBusiness(ctx context.Context, businessID, id string) (*models.Invoice, error)
}

var ErrInvalidPartyGroupMember = errors.New("invalid party group member")

func rejectInvoiceSubscriptionAutoSend(autoSend bool) error {
	if !autoSend {
		return nil
	}
	return fmt.Errorf("invoice subscription auto-send requires the issue and delivery workflow: %w", models.ErrInvalidInvoiceLifecycle)
}

func validateInvoiceSubscriptionGeneration(subscription *models.InvoiceSubscription) error {
	if subscription.Status != models.InvoiceSubscriptionStatusActive {
		return fmt.Errorf("invoice subscription is not active")
	}
	if err := rejectInvoiceSubscriptionAutoSend(subscription.AutoSend); err != nil {
		return err
	}
	return nil
}

func NewBillingOpsService(
	cfg *config.Config,
	db *gorm.DB,
	customerRepo interfaces.CustomerRepository,
	vendorRepo interfaces.VendorRepository,
	productRepo interfaces.ProductRepository,
	invoices *InvoiceService,
	documents *DocumentService,
	s3 *S3Service,
	permissions PermissionChecker,
	log *logger.Logger,
) *BillingOpsService {
	return &BillingOpsService{
		cfg:          cfg,
		db:           db,
		customerRepo: customerRepo,
		vendorRepo:   vendorRepo,
		productRepo:  productRepo,
		invoices:     invoices,
		documents:    documents,
		s3:           s3,
		permissions:  permissions,
		log:          log,
	}
}

type PriceListItemInput struct {
	EntityType string                 `json:"entity_type,omitempty"`
	ProductID  string                 `json:"product_id,omitempty"`
	VariantID  string                 `json:"variant_id,omitempty"`
	Price      float64                `json:"price"`
	MRP        float64                `json:"mrp"`
	CessRate   float64                `json:"cess_rate"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

type PriceListAssignmentInput struct {
	ScopeType string                 `json:"scope_type" binding:"required"`
	ScopeID   string                 `json:"scope_id" binding:"required"`
	Priority  int                    `json:"priority"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type CreatePriceListInput struct {
	BusinessID  string                     `json:"business_id,omitempty"`
	Name        string                     `json:"name" binding:"required"`
	Code        string                     `json:"code" binding:"required"`
	Description string                     `json:"description"`
	Currency    string                     `json:"currency"`
	IsDefault   bool                       `json:"is_default"`
	IsActive    *bool                      `json:"is_active,omitempty"`
	Metadata    map[string]interface{}     `json:"metadata,omitempty"`
	Items       []PriceListItemInput       `json:"items,omitempty"`
	Assignments []PriceListAssignmentInput `json:"assignments,omitempty"`
}

type UpdatePriceListInput struct {
	Name        string                     `json:"name"`
	Code        string                     `json:"code"`
	Description string                     `json:"description"`
	Currency    string                     `json:"currency"`
	IsDefault   *bool                      `json:"is_default,omitempty"`
	IsActive    *bool                      `json:"is_active,omitempty"`
	Metadata    map[string]interface{}     `json:"metadata,omitempty"`
	Items       []PriceListItemInput       `json:"items,omitempty"`
	Assignments []PriceListAssignmentInput `json:"assignments,omitempty"`
}

type PriceListDetail struct {
	PriceList   *models.PriceList             `json:"price_list"`
	Items       []*models.PriceListItem       `json:"items,omitempty"`
	Assignments []*models.PriceListAssignment `json:"assignments,omitempty"`
}

func (s *BillingOpsService) CreatePriceList(ctx context.Context, input CreatePriceListInput) (*PriceListDetail, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	priceList := &models.PriceList{
		BusinessID:  input.BusinessID,
		Name:        input.Name,
		Code:        strings.ToUpper(strings.TrimSpace(input.Code)),
		Description: input.Description,
		Currency:    defaultCurrency(input.Currency),
		IsDefault:   input.IsDefault,
		IsActive:    true,
		Metadata:    mustMarshalMap(input.Metadata),
	}
	if input.IsActive != nil {
		priceList.IsActive = *input.IsActive
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if priceList.IsDefault {
			if err := tx.Model(&models.PriceList{}).
				Where("business_id = ? AND deleted_at IS NULL", input.BusinessID).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(priceList).Error; err != nil {
			return err
		}
		if err := s.replacePriceListItemsTx(tx, priceList.ID, input.Items); err != nil {
			return err
		}
		if err := s.replacePriceListAssignmentsTx(tx, input.BusinessID, priceList.ID, input.Assignments); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	_ = recordActivityLog(ctx, s.db, input.BusinessID, "price_list", priceList.ID, "created", "", priceList, nil, nil)
	return s.GetPriceList(ctx, input.BusinessID, priceList.ID)
}

func (s *BillingOpsService) replacePriceListItemsTx(tx *gorm.DB, priceListID string, items []PriceListItemInput) error {
	if err := tx.Where("price_list_id = ?", priceListID).Delete(&models.PriceListItem{}).Error; err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	records := make([]models.PriceListItem, 0, len(items))
	for _, item := range items {
		record := models.PriceListItem{
			PriceListID: priceListID,
			EntityType:  coalesceString(item.EntityType, models.PriceListEntityProduct),
			Price:       item.Price,
			MRP:         item.MRP,
			CessRate:    item.CessRate,
			Metadata:    mustMarshalMap(item.Metadata),
		}
		if item.ProductID != "" {
			record.ProductID = &item.ProductID
		}
		if item.VariantID != "" {
			record.VariantID = &item.VariantID
			record.EntityType = models.PriceListEntityVariant
		}
		records = append(records, record)
	}
	return tx.Create(&records).Error
}

func (s *BillingOpsService) replacePriceListAssignmentsTx(tx *gorm.DB, businessID, priceListID string, assignments []PriceListAssignmentInput) error {
	if err := tx.Where("price_list_id = ?", priceListID).Delete(&models.PriceListAssignment{}).Error; err != nil {
		return err
	}
	if len(assignments) == 0 {
		return nil
	}
	records := make([]models.PriceListAssignment, 0, len(assignments))
	for _, assignment := range assignments {
		records = append(records, models.PriceListAssignment{
			BusinessID:  businessID,
			PriceListID: priceListID,
			ScopeType:   assignment.ScopeType,
			ScopeID:     assignment.ScopeID,
			Priority:    assignment.Priority,
			Metadata:    mustMarshalMap(assignment.Metadata),
		})
	}
	return tx.Create(&records).Error
}

func (s *BillingOpsService) GetPriceList(ctx context.Context, businessID, id string) (*PriceListDetail, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var priceList models.PriceList
	if err := s.db.WithContext(ctx).
		Preload("Items").
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		First(&priceList).Error; err != nil {
		return nil, err
	}
	var assignments []models.PriceListAssignment
	if err := s.db.WithContext(ctx).
		Where("price_list_id = ? AND deleted_at IS NULL", id).
		Order("priority DESC, created_at DESC").
		Find(&assignments).Error; err != nil {
		return nil, err
	}
	result := make([]*models.PriceListAssignment, 0, len(assignments))
	for i := range assignments {
		result = append(result, &assignments[i])
	}
	return &PriceListDetail{PriceList: &priceList, Items: priceList.Items, Assignments: result}, nil
}

func (s *BillingOpsService) ListPriceLists(ctx context.Context, businessID string, page, limit int) ([]*models.PriceList, int64, error) {
	if s.db == nil {
		return nil, 0, fmt.Errorf("database is not configured")
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	var total int64
	query := s.db.WithContext(ctx).Model(&models.PriceList{}).
		Where("business_id = ? AND deleted_at IS NULL", businessID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.PriceList
	if err := query.Preload("Items").
		Order("is_default DESC, updated_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	result := make([]*models.PriceList, 0, len(rows))
	for i := range rows {
		result = append(result, &rows[i])
	}
	return result, total, nil
}

func (s *BillingOpsService) UpdatePriceList(ctx context.Context, businessID, id string, input UpdatePriceListInput) (*PriceListDetail, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var priceList models.PriceList
	if err := s.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		First(&priceList).Error; err != nil {
		return nil, err
	}
	if input.Name != "" {
		priceList.Name = input.Name
	}
	if input.Code != "" {
		priceList.Code = strings.ToUpper(strings.TrimSpace(input.Code))
	}
	if input.Description != "" {
		priceList.Description = input.Description
	}
	if input.Currency != "" {
		priceList.Currency = defaultCurrency(input.Currency)
	}
	if input.IsDefault != nil {
		priceList.IsDefault = *input.IsDefault
	}
	if input.IsActive != nil {
		priceList.IsActive = *input.IsActive
	}
	if input.Metadata != nil {
		priceList.Metadata = mustMarshalMap(input.Metadata)
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if priceList.IsDefault {
			if err := tx.Model(&models.PriceList{}).
				Where("business_id = ? AND id <> ? AND deleted_at IS NULL", businessID, id).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}
		if err := tx.Save(&priceList).Error; err != nil {
			return err
		}
		if input.Items != nil {
			if err := s.replacePriceListItemsTx(tx, id, input.Items); err != nil {
				return err
			}
		}
		if input.Assignments != nil {
			if err := s.replacePriceListAssignmentsTx(tx, businessID, id, input.Assignments); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	_ = recordActivityLog(ctx, s.db, businessID, "price_list", id, "updated", "", priceList, nil, nil)
	return s.GetPriceList(ctx, businessID, id)
}

func (s *BillingOpsService) DeletePriceList(ctx context.Context, businessID, id string) error {
	if s.db == nil {
		return fmt.Errorf("database is not configured")
	}
	if err := s.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		Delete(&models.PriceList{}).Error; err != nil {
		return err
	}
	_ = recordActivityLog(ctx, s.db, businessID, "price_list", id, "deleted", "", nil, nil, nil)
	return nil
}

type PartyGroupMemberInput struct {
	PartyType string `json:"party_type" binding:"required"`
	PartyID   string `json:"party_id" binding:"required"`
}

type CreatePartyGroupInput struct {
	BusinessID  string                  `json:"business_id,omitempty"`
	Name        string                  `json:"name" binding:"required"`
	Description string                  `json:"description"`
	Members     []PartyGroupMemberInput `json:"members,omitempty"`
}

type UpdatePartyGroupInput struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Members     []PartyGroupMemberInput `json:"members,omitempty"`
}

type PartyGroupLedgerSummary struct {
	GroupID          string                     `json:"group_id"`
	Members          []*models.PartyGroupMember `json:"members"`
	InvoiceCount     int64                      `json:"invoice_count"`
	DocumentCount    int64                      `json:"document_count"`
	TransactionCount int64                      `json:"transaction_count"`
	TotalAmount      float64                    `json:"total_amount"`
	PaidAmount       float64                    `json:"paid_amount"`
	BalanceDue       float64                    `json:"balance_due"`
	ReceivableTotal  float64                    `json:"receivable_total"`
	PayableTotal     float64                    `json:"payable_total"`
	NetBalance       float64                    `json:"net_balance"`
}

type InvoiceSubscriptionRunFeedItem struct {
	ID                  string     `json:"id"`
	SubscriptionID      string     `json:"subscription_id"`
	SubscriptionName    string     `json:"subscription_name"`
	SubscriptionStatus  string     `json:"subscription_status"`
	SubscriptionCadence string     `json:"subscription_cadence"`
	Status              string     `json:"status"`
	ScheduledFor        time.Time  `json:"scheduled_for"`
	AttemptCount        int        `json:"attempt_count"`
	LastError           *string    `json:"last_error,omitempty"`
	InvoiceID           *string    `json:"invoice_id,omitempty"`
	DocumentID          *string    `json:"document_id,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
}

type InvoiceSubscriptionRunStats struct {
	ActiveCount int64 `json:"active_count"`
	PausedCount int64 `json:"paused_count"`
	TotalRuns   int64 `json:"total_runs"`
	FailedRuns  int64 `json:"failed_runs"`
}

func (s *BillingOpsService) CreatePartyGroup(ctx context.Context, input CreatePartyGroupInput) (*models.PartyGroup, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if err := s.validatePartyGroupMembers(ctx, input.BusinessID, input.Members); err != nil {
		return nil, err
	}
	group := &models.PartyGroup{
		BusinessID:  input.BusinessID,
		Name:        input.Name,
		Description: input.Description,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(group).Error; err != nil {
			return err
		}
		return s.replacePartyGroupMembersTx(tx, input.BusinessID, group.ID, input.Members)
	})
	if err != nil {
		return nil, err
	}
	_ = recordActivityLog(ctx, s.db, input.BusinessID, "party_group", group.ID, "created", "", group, nil, nil)
	return s.GetPartyGroup(ctx, input.BusinessID, group.ID)
}

func (s *BillingOpsService) replacePartyGroupMembersTx(tx *gorm.DB, businessID, groupID string, members []PartyGroupMemberInput) error {
	if err := tx.Where("party_group_id = ?", groupID).Delete(&models.PartyGroupMember{}).Error; err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}
	records := make([]models.PartyGroupMember, 0, len(members))
	for _, member := range members {
		records = append(records, models.PartyGroupMember{
			PartyGroupID: groupID,
			BusinessID:   businessID,
			PartyType:    strings.TrimSpace(member.PartyType),
			PartyID:      strings.TrimSpace(member.PartyID),
		})
	}
	return tx.Create(&records).Error
}

func (s *BillingOpsService) validatePartyGroupMembers(ctx context.Context, businessID string, members []PartyGroupMemberInput) error {
	seen := map[string]struct{}{}
	for _, member := range members {
		partyType := strings.TrimSpace(member.PartyType)
		partyID := strings.TrimSpace(member.PartyID)
		if partyID == "" {
			return fmt.Errorf("%w: party_id is required", ErrInvalidPartyGroupMember)
		}
		switch partyType {
		case models.DocumentPartyTypeCustomer, models.DocumentPartyTypeVendor:
		default:
			return fmt.Errorf("%w: unsupported member type %q", ErrInvalidPartyGroupMember, partyType)
		}
		memberKey := partyType + ":" + partyID
		if _, exists := seen[memberKey]; exists {
			return fmt.Errorf("%w: duplicate member %s", ErrInvalidPartyGroupMember, partyID)
		}
		seen[memberKey] = struct{}{}

		var count int64
		switch partyType {
		case models.DocumentPartyTypeCustomer:
			if err := s.db.WithContext(ctx).Model(&models.Customer{}).
				Where("id = ? AND business_id = ? AND deleted_at IS NULL", partyID, businessID).
				Count(&count).Error; err != nil {
				return err
			}
		case models.DocumentPartyTypeVendor:
			if err := s.db.WithContext(ctx).Model(&models.Vendor{}).
				Where("id = ? AND business_id = ? AND deleted_at IS NULL", partyID, businessID).
				Count(&count).Error; err != nil {
				return err
			}
		}
		if count == 0 {
			return fmt.Errorf("%w: %s %s not found", ErrInvalidPartyGroupMember, partyType, partyID)
		}
	}
	return nil
}

func (s *BillingOpsService) GetPartyGroup(ctx context.Context, businessID, id string) (*models.PartyGroup, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var group models.PartyGroup
	if err := s.db.WithContext(ctx).
		Preload("Members").
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		First(&group).Error; err != nil {
		return nil, err
	}
	return &group, nil
}

func (s *BillingOpsService) ListPartyGroups(ctx context.Context, businessID string, page, limit int) ([]*models.PartyGroup, int64, error) {
	if s.db == nil {
		return nil, 0, fmt.Errorf("database is not configured")
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	var total int64
	query := s.db.WithContext(ctx).Model(&models.PartyGroup{}).
		Where("business_id = ? AND deleted_at IS NULL", businessID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.PartyGroup
	if err := query.Preload("Members").
		Order("updated_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	result := make([]*models.PartyGroup, 0, len(rows))
	for i := range rows {
		result = append(result, &rows[i])
	}
	if err := s.decoratePartyGroups(ctx, businessID, result); err != nil {
		return nil, 0, err
	}
	return result, total, nil
}

func (s *BillingOpsService) UpdatePartyGroup(ctx context.Context, businessID, id string, input UpdatePartyGroupInput) (*models.PartyGroup, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	group, err := s.GetPartyGroup(ctx, businessID, id)
	if err != nil {
		return nil, err
	}
	if input.Members != nil {
		if err := s.validatePartyGroupMembers(ctx, businessID, input.Members); err != nil {
			return nil, err
		}
	}
	if input.Name != "" {
		group.Name = input.Name
	}
	if input.Description != "" {
		group.Description = input.Description
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(group).Error; err != nil {
			return err
		}
		if input.Members != nil {
			return s.replacePartyGroupMembersTx(tx, businessID, id, input.Members)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	_ = recordActivityLog(ctx, s.db, businessID, "party_group", id, "updated", "", group, nil, nil)
	return s.GetPartyGroup(ctx, businessID, id)
}

func (s *BillingOpsService) DeletePartyGroup(ctx context.Context, businessID, id string) error {
	if s.db == nil {
		return fmt.Errorf("database is not configured")
	}
	if err := s.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		Delete(&models.PartyGroup{}).Error; err != nil {
		return err
	}
	_ = recordActivityLog(ctx, s.db, businessID, "party_group", id, "deleted", "", nil, nil, nil)
	return nil
}

func (s *BillingOpsService) GetPartyGroupLedger(ctx context.Context, businessID, id string) (*PartyGroupLedgerSummary, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	group, err := s.GetPartyGroup(ctx, businessID, id)
	if err != nil {
		return nil, err
	}
	return s.buildPartyGroupLedgerSummary(ctx, businessID, group)
}

type ActivityLogFilter struct {
	BusinessID string
	EntityType string
	EntityID   string
	Action     string
	Page       int
	Limit      int
}

func (s *BillingOpsService) ListActivityLogs(ctx context.Context, filter ActivityLogFilter) ([]*models.ActivityLog, int64, error) {
	if s.db == nil {
		return nil, 0, fmt.Errorf("database is not configured")
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 100
	}
	query := s.db.WithContext(ctx).Model(&models.ActivityLog{}).
		Where("business_id = ? AND deleted_at IS NULL", filter.BusinessID)
	if filter.EntityType != "" {
		query = query.Where("entity_type = ?", filter.EntityType)
	}
	if filter.EntityID != "" {
		query = query.Where("entity_id = ?", filter.EntityID)
	}
	if filter.Action != "" {
		query = query.Where("action = ?", filter.Action)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.ActivityLog
	if err := query.Order("created_at DESC").
		Offset((filter.Page - 1) * filter.Limit).
		Limit(filter.Limit).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	result := make([]*models.ActivityLog, 0, len(rows))
	for i := range rows {
		result = append(result, &rows[i])
	}
	return result, total, nil
}

type CreateSignatureProfileInput struct {
	BusinessID    string                 `json:"business_id,omitempty"`
	Name          string                 `json:"name" binding:"required"`
	Provider      string                 `json:"provider"`
	SignerName    string                 `json:"signer_name"`
	CertificateSN string                 `json:"certificate_sn"`
	FileName      string                 `json:"file_name"`
	FileContent   []byte                 `json:"-"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

func (s *BillingOpsService) CreateSignatureProfile(ctx context.Context, input CreateSignatureProfileInput) (*models.SignatureProfile, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	profile := &models.SignatureProfile{
		BusinessID:    input.BusinessID,
		Name:          input.Name,
		Provider:      input.Provider,
		SignerName:    input.SignerName,
		CertificateSN: input.CertificateSN,
		FileName:      input.FileName,
		Metadata:      mustMarshalMap(input.Metadata),
		IsActive:      true,
	}
	if len(input.FileContent) > 0 && s.s3 != nil && s.cfg != nil {
		key, err := tenantArtifactObjectKey("signature-profiles", input.BusinessID, uuid.NewString(), input.FileName)
		if err != nil {
			return nil, err
		}
		if err := s.s3.Upload(ctx, s.cfg.S3.BucketInvoices, key, input.FileContent, "application/x-pkcs12"); err != nil {
			return nil, err
		}
		profile.FileKey = key
	}
	if err := s.db.WithContext(ctx).Create(profile).Error; err != nil {
		return nil, err
	}
	_ = recordActivityLog(ctx, s.db, input.BusinessID, "signature_profile", profile.ID, "created", "", profile, nil, nil)
	return profile, nil
}

func (s *BillingOpsService) ListSignatureProfiles(ctx context.Context, businessID string) ([]*models.SignatureProfile, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var rows []models.SignatureProfile
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("updated_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*models.SignatureProfile, 0, len(rows))
	for i := range rows {
		result = append(result, &rows[i])
	}
	return result, nil
}

func (s *BillingOpsService) GetSignatureProfile(ctx context.Context, businessID, id string) (*models.SignatureProfile, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var profile models.SignatureProfile
	if err := s.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		First(&profile).Error; err != nil {
		return nil, err
	}
	return &profile, nil
}

func (s *BillingOpsService) SignInvoice(ctx context.Context, businessID, invoiceID, profileID, passphrase, reason string) (*models.SignedDocumentArtifact, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	invoice, err := s.invoices.GetByBusiness(ctx, businessID, invoiceID)
	if err != nil {
		return nil, err
	}
	if invoice.SignedAt != nil {
		return nil, fmt.Errorf("invoice already signed")
	}
	profile, err := s.GetSignatureProfile(ctx, businessID, profileID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	artifact := &models.SignedDocumentArtifact{
		BusinessID:         businessID,
		InvoiceID:          &invoice.ID,
		SignatureProfileID: profile.ID,
		FileName:           fmt.Sprintf("signed-%s.pdf", strings.ToLower(models.StringValue(invoice.InvoiceNo))),
		FileKey:            path.Join("signed-documents", businessID, "invoices", invoice.ID, fmt.Sprintf("%d.pdf", now.UnixNano())),
		SourcePDFURL:       invoice.PDFURL,
		SignedPDFURL:       invoice.PDFURL,
		Metadata: mustMarshalMap(map[string]interface{}{
			"reason":             reason,
			"certificate_sn":     profile.CertificateSN,
			"provider":           profile.Provider,
			"passphrase_present": passphrase != "",
		}),
	}
	invoice.SignedAt = &now
	invoice.SignedByProfileID = &profile.ID
	invoice.SignMetadata = mustMarshalMap(map[string]interface{}{
		"reason":             reason,
		"artifact_file_key":  artifact.FileKey,
		"certificate_sn":     profile.CertificateSN,
		"provider":           profile.Provider,
		"passphrase_present": passphrase != "",
	})
	profile.LastUsedAt = &now
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(artifact).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Invoice{}).
			Where("id = ? AND business_id = ?", invoice.ID, businessID).
			Updates(map[string]interface{}{
				"signed_at":            invoice.SignedAt,
				"signed_by_profile_id": invoice.SignedByProfileID,
				"sign_metadata":        invoice.SignMetadata,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.SignatureProfile{}).Where("id = ?", profile.ID).Update("last_used_at", now).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.documents != nil {
		_ = s.documents.MirrorLegacyInvoice(ctx, invoice)
	}
	_ = recordActivityLog(ctx, s.db, businessID, "invoice", invoice.ID, "signed", reason, invoice, nil, map[string]interface{}{"signature_profile_id": profile.ID})
	return artifact, nil
}

func (s *BillingOpsService) SignDocument(ctx context.Context, businessID, documentID, profileID, passphrase, reason string) (*models.SignedDocumentArtifact, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	document, err := s.documents.GetByBusiness(ctx, businessID, documentID)
	if err != nil {
		return nil, err
	}
	if document.SignedAt != nil {
		return nil, fmt.Errorf("document already signed")
	}
	if document.PartyType != models.DocumentPartyTypeCustomer {
		return nil, fmt.Errorf("only outward customer-facing documents can be signed")
	}
	profile, err := s.GetSignatureProfile(ctx, businessID, profileID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	artifact := &models.SignedDocumentArtifact{
		BusinessID:         businessID,
		DocumentID:         &document.ID,
		SignatureProfileID: profile.ID,
		FileName:           fmt.Sprintf("signed-%s.pdf", strings.ToLower(document.SerialNumber)),
		FileKey:            path.Join("signed-documents", businessID, "documents", document.ID, fmt.Sprintf("%d.pdf", now.UnixNano())),
		SourcePDFURL:       document.PDFURL,
		SignedPDFURL:       document.PDFURL,
		Metadata: mustMarshalMap(map[string]interface{}{
			"reason":             reason,
			"certificate_sn":     profile.CertificateSN,
			"provider":           profile.Provider,
			"passphrase_present": passphrase != "",
		}),
	}
	document.SignedAt = &now
	document.SignedByProfileID = &profile.ID
	document.SignMetadata = mustMarshalMap(map[string]interface{}{
		"reason":             reason,
		"artifact_file_key":  artifact.FileKey,
		"certificate_sn":     profile.CertificateSN,
		"provider":           profile.Provider,
		"passphrase_present": passphrase != "",
	})
	profile.LastUsedAt = &now
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(artifact).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Document{}).
			Where("id = ? AND business_id = ?", document.ID, businessID).
			Updates(map[string]interface{}{
				"signed_at":            document.SignedAt,
				"signed_by_profile_id": document.SignedByProfileID,
				"sign_metadata":        document.SignMetadata,
			}).Error; err != nil {
			return err
		}
		return tx.Model(&models.SignatureProfile{}).Where("id = ?", profile.ID).Update("last_used_at", now).Error
	})
	if err != nil {
		return nil, err
	}
	_ = recordActivityLog(ctx, s.db, businessID, "document", document.ID, "signed", reason, document, nil, map[string]interface{}{"signature_profile_id": profile.ID})
	return artifact, nil
}

type CreateBulkJobInput struct {
	BusinessID     string                 `json:"business_id,omitempty"`
	CreatedBy      string                 `json:"created_by,omitempty"`
	JobType        string                 `json:"job_type" binding:"required"`
	Action         string                 `json:"action,omitempty"`
	FileName       string                 `json:"file_name,omitempty"`
	ContentType    string                 `json:"content_type,omitempty"`
	FileContent    []byte                 `json:"-"`
	EntityIDs      []string               `json:"entity_ids,omitempty"`
	RequestPayload map[string]interface{} `json:"request_payload,omitempty"`
}

func (s *BillingOpsService) CreateBulkJob(ctx context.Context, input CreateBulkJobInput) (*models.BulkJob, error) {
	switch input.JobType {
	case models.BulkJobTypeImportCustomers:
		if err := requireMutationPermission(ctx, s.permissions, input.BusinessID, PermissionCustomersCreate); err != nil {
			return nil, err
		}
	case models.BulkJobTypeImportVendors:
		if err := requireMutationPermission(ctx, s.permissions, input.BusinessID, PermissionVendorsCreate); err != nil {
			return nil, err
		}
	}
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	job := &models.BulkJob{
		BusinessID:     input.BusinessID,
		CreatedBy:      input.CreatedBy,
		JobType:        input.JobType,
		Action:         input.Action,
		Status:         models.BulkJobStatusQueued,
		FileName:       input.FileName,
		ContentType:    input.ContentType,
		RequestPayload: mustMarshalMap(input.RequestPayload),
	}
	now := time.Now().UTC()
	job.QueuedAt = &now
	if len(input.FileContent) > 0 && s.s3 != nil && s.cfg != nil {
		key, err := tenantArtifactObjectKey("bulk-jobs", input.BusinessID, uuid.NewString(), input.FileName)
		if err != nil {
			return nil, err
		}
		if err := s.s3.Upload(ctx, s.cfg.S3.BucketInvoices, key, input.FileContent, input.ContentType); err != nil {
			return nil, err
		}
		job.FileKey = key
	}
	if strings.Contains(strings.ToLower(input.ContentType), "csv") || strings.HasSuffix(strings.ToLower(input.FileName), ".csv") {
		reader := csv.NewReader(strings.NewReader(string(input.FileContent)))
		rowNumber := 0
		rows := make([]*models.BulkJobRow, 0)
		for {
			record, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
			rowNumber++
			rows = append(rows, &models.BulkJobRow{
				RowNumber: rowNumber,
				Status:    models.BulkJobStatusQueued,
				Input:     mustMarshalAny(record, "[]"),
			})
		}
		job.TotalRows = len(rows)
		job.Rows = rows
	} else {
		job.TotalRows = len(input.EntityIDs)
		for idx, entityID := range input.EntityIDs {
			copyID := entityID
			job.Rows = append(job.Rows, &models.BulkJobRow{
				RowNumber: idx + 1,
				Status:    models.BulkJobStatusQueued,
				EntityID:  &copyID,
				Input: mustMarshalMap(map[string]interface{}{
					"entity_id": entityID,
				}),
			})
		}
	}
	if err := s.db.WithContext(ctx).Create(job).Error; err != nil {
		return nil, err
	}
	_ = recordActivityLog(ctx, s.db, input.BusinessID, "bulk_job", job.ID, "created", "", job, nil, nil)
	return job, nil
}

func (s *BillingOpsService) ListBulkJobs(ctx context.Context, businessID string, page, limit int) ([]*models.BulkJob, int64, error) {
	if s.db == nil {
		return nil, 0, fmt.Errorf("database is not configured")
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	query := s.db.WithContext(ctx).Model(&models.BulkJob{}).
		Where("business_id = ? AND deleted_at IS NULL", businessID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.BulkJob
	if err := query.Preload("Rows").Preload("Artifacts").
		Order("created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	result := make([]*models.BulkJob, 0, len(rows))
	for i := range rows {
		result = append(result, &rows[i])
	}
	return result, total, nil
}

func (s *BillingOpsService) GetBulkJob(ctx context.Context, businessID, id string) (*models.BulkJob, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var job models.BulkJob
	if err := s.db.WithContext(ctx).
		Preload("Rows").
		Preload("Artifacts").
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

type InvoiceSubscriptionLineInput struct {
	ProductID        string                 `json:"product_id,omitempty"`
	VariantID        string                 `json:"variant_id,omitempty"`
	WarehouseID      string                 `json:"warehouse_id,omitempty"`
	Description      string                 `json:"description" binding:"required"`
	Quantity         float64                `json:"quantity" binding:"required,gt=0"`
	FreeQuantity     float64                `json:"free_quantity"`
	UnitPrice        float64                `json:"unit_price"`
	MRP              float64                `json:"mrp"`
	DiscountAmount   float64                `json:"discount_amount"`
	TaxRate          float64                `json:"tax_rate"`
	CessRate         float64                `json:"cess_rate"`
	CustomFields     map[string]interface{} `json:"custom_fields,omitempty"`
	AdditionalCharge map[string]interface{} `json:"additional_charge,omitempty"`
	Position         int                    `json:"position"`
}

type CreateInvoiceSubscriptionInput struct {
	BusinessID  string                         `json:"business_id,omitempty"`
	CustomerID  string                         `json:"customer_id" binding:"required,uuid"`
	Name        string                         `json:"name" binding:"required"`
	Cadence     string                         `json:"cadence" binding:"required"`
	Timezone    string                         `json:"timezone"`
	StartDate   time.Time                      `json:"start_date" binding:"required"`
	EndDate     *time.Time                     `json:"end_date,omitempty"`
	AutoSend    bool                           `json:"auto_send"`
	PricePolicy string                         `json:"price_policy"`
	PriceListID string                         `json:"price_list_id,omitempty"`
	Currency    string                         `json:"currency"`
	Notes       string                         `json:"notes"`
	Metadata    map[string]interface{}         `json:"metadata,omitempty"`
	Lines       []InvoiceSubscriptionLineInput `json:"lines" binding:"required,min=1,dive"`
}

type UpdateInvoiceSubscriptionInput struct {
	Name        string                         `json:"name"`
	Cadence     string                         `json:"cadence"`
	Timezone    string                         `json:"timezone"`
	StartDate   *time.Time                     `json:"start_date,omitempty"`
	EndDate     *time.Time                     `json:"end_date,omitempty"`
	AutoSend    *bool                          `json:"auto_send,omitempty"`
	PricePolicy string                         `json:"price_policy"`
	PriceListID *string                        `json:"price_list_id,omitempty"`
	Currency    string                         `json:"currency"`
	Notes       string                         `json:"notes"`
	Metadata    map[string]interface{}         `json:"metadata,omitempty"`
	Lines       []InvoiceSubscriptionLineInput `json:"lines,omitempty"`
}

func (s *BillingOpsService) CreateInvoiceSubscription(ctx context.Context, input CreateInvoiceSubscriptionInput) (*models.InvoiceSubscription, error) {
	if err := rejectInvoiceSubscriptionAutoSend(input.AutoSend); err != nil {
		return nil, err
	}
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if _, err := s.customerRepo.GetByID(ctx, input.CustomerID, input.BusinessID); err != nil {
		return nil, err
	}
	pricePolicy := coalesceString(input.PricePolicy, models.InvoiceSubscriptionPricePolicyFreeze)
	var explicitPriceListID *string
	if input.PriceListID != "" {
		explicitPriceListID = &input.PriceListID
	}
	priceListID, err := resolvePriceListID(ctx, s.db, s.customerRepo, s.vendorRepo, input.BusinessID, models.DocumentPartyTypeCustomer, input.CustomerID, explicitPriceListID, nil)
	if err != nil {
		return nil, err
	}
	nextRunAt := input.StartDate
	subscription := &models.InvoiceSubscription{
		BusinessID:  input.BusinessID,
		CustomerID:  input.CustomerID,
		Name:        input.Name,
		Status:      models.InvoiceSubscriptionStatusActive,
		Cadence:     input.Cadence,
		Timezone:    coalesceString(input.Timezone, "Asia/Kolkata"),
		StartDate:   input.StartDate,
		EndDate:     input.EndDate,
		NextRunAt:   &nextRunAt,
		AutoSend:    input.AutoSend,
		PricePolicy: pricePolicy,
		PriceListID: priceListID,
		Currency:    defaultCurrency(input.Currency),
		Notes:       input.Notes,
		Metadata:    mustMarshalMap(input.Metadata),
	}
	lines := make([]*models.InvoiceSubscriptionLine, 0, len(input.Lines))
	for _, lineInput := range input.Lines {
		pricing, err := resolveLinePricing(ctx, s.db, s.productRepo, input.BusinessID, priceListID, lineInput.ProductID, lineInput.VariantID, stringPointer(lineInput.WarehouseID))
		if err != nil {
			return nil, err
		}
		line := &models.InvoiceSubscriptionLine{
			Description:      lineInput.Description,
			Quantity:         lineInput.Quantity,
			FreeQuantity:     lineInput.FreeQuantity,
			UnitPrice:        lineInput.UnitPrice,
			MRP:              lineInput.MRP,
			DiscountAmount:   lineInput.DiscountAmount,
			TaxRate:          lineInput.TaxRate,
			CessRate:         lineInput.CessRate,
			CustomFields:     mustMarshalMap(lineInput.CustomFields),
			AdditionalCharge: mustMarshalMap(lineInput.AdditionalCharge),
			Position:         lineInput.Position,
		}
		if lineInput.ProductID != "" {
			line.ProductID = &lineInput.ProductID
		}
		if lineInput.VariantID != "" {
			line.VariantID = &lineInput.VariantID
		}
		if lineInput.WarehouseID != "" {
			line.WarehouseID = &lineInput.WarehouseID
		}
		if pricePolicy == models.InvoiceSubscriptionPricePolicyFreeze {
			if line.UnitPrice <= 0 {
				line.UnitPrice = pricing.UnitPrice
			}
			if line.MRP <= 0 {
				line.MRP = pricing.MRP
			}
			if line.CessRate <= 0 {
				line.CessRate = pricing.CessRate
			}
		}
		lines = append(lines, line)
	}
	subscription.Lines = lines
	if err := s.db.WithContext(ctx).Create(subscription).Error; err != nil {
		return nil, err
	}
	_ = recordActivityLog(ctx, s.db, input.BusinessID, "invoice_subscription", subscription.ID, "created", "", subscription, nil, nil)
	return s.GetInvoiceSubscription(ctx, input.BusinessID, subscription.ID)
}

func (s *BillingOpsService) GetInvoiceSubscription(ctx context.Context, businessID, id string) (*models.InvoiceSubscription, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	var subscription models.InvoiceSubscription
	if err := s.db.WithContext(ctx).
		Preload("Lines").
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		First(&subscription).Error; err != nil {
		return nil, err
	}
	return &subscription, nil
}

func (s *BillingOpsService) ListInvoiceSubscriptions(ctx context.Context, businessID string, page, limit int) ([]*models.InvoiceSubscription, int64, error) {
	if s.db == nil {
		return nil, 0, fmt.Errorf("database is not configured")
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	query := s.db.WithContext(ctx).Model(&models.InvoiceSubscription{}).
		Where("business_id = ? AND deleted_at IS NULL", businessID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.InvoiceSubscription
	if err := query.Preload("Lines").
		Order("updated_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	result := make([]*models.InvoiceSubscription, 0, len(rows))
	for i := range rows {
		result = append(result, &rows[i])
	}
	if err := s.decorateInvoiceSubscriptions(ctx, businessID, result); err != nil {
		return nil, 0, err
	}
	return result, total, nil
}

func (s *BillingOpsService) UpdateInvoiceSubscription(ctx context.Context, businessID, id string, input UpdateInvoiceSubscriptionInput) (*models.InvoiceSubscription, error) {
	if input.AutoSend != nil {
		if err := rejectInvoiceSubscriptionAutoSend(*input.AutoSend); err != nil {
			return nil, err
		}
	}
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	subscription, err := s.GetInvoiceSubscription(ctx, businessID, id)
	if err != nil {
		return nil, err
	}
	if input.Name != "" {
		subscription.Name = input.Name
	}
	if input.Cadence != "" {
		subscription.Cadence = input.Cadence
	}
	if input.Timezone != "" {
		subscription.Timezone = input.Timezone
	}
	if input.StartDate != nil {
		subscription.StartDate = *input.StartDate
		if subscription.NextRunAt == nil {
			subscription.NextRunAt = input.StartDate
		}
	}
	if input.EndDate != nil {
		subscription.EndDate = input.EndDate
	}
	if input.AutoSend != nil {
		subscription.AutoSend = *input.AutoSend
	}
	if input.PricePolicy != "" {
		subscription.PricePolicy = input.PricePolicy
	}
	if input.PriceListID != nil {
		subscription.PriceListID = input.PriceListID
	}
	if input.Currency != "" {
		subscription.Currency = defaultCurrency(input.Currency)
	}
	if input.Notes != "" {
		subscription.Notes = input.Notes
	}
	if input.Metadata != nil {
		subscription.Metadata = mustMarshalMap(input.Metadata)
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(subscription).Error; err != nil {
			return err
		}
		if input.Lines != nil {
			if err := tx.Where("subscription_id = ?", id).Delete(&models.InvoiceSubscriptionLine{}).Error; err != nil {
				return err
			}
			for _, lineInput := range input.Lines {
				line := models.InvoiceSubscriptionLine{
					SubscriptionID:   id,
					Description:      lineInput.Description,
					Quantity:         lineInput.Quantity,
					FreeQuantity:     lineInput.FreeQuantity,
					UnitPrice:        lineInput.UnitPrice,
					MRP:              lineInput.MRP,
					DiscountAmount:   lineInput.DiscountAmount,
					TaxRate:          lineInput.TaxRate,
					CessRate:         lineInput.CessRate,
					CustomFields:     mustMarshalMap(lineInput.CustomFields),
					AdditionalCharge: mustMarshalMap(lineInput.AdditionalCharge),
					Position:         lineInput.Position,
				}
				if lineInput.ProductID != "" {
					line.ProductID = &lineInput.ProductID
				}
				if lineInput.VariantID != "" {
					line.VariantID = &lineInput.VariantID
				}
				if lineInput.WarehouseID != "" {
					line.WarehouseID = &lineInput.WarehouseID
				}
				if err := tx.Create(&line).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	_ = recordActivityLog(ctx, s.db, businessID, "invoice_subscription", id, "updated", "", subscription, nil, nil)
	return s.GetInvoiceSubscription(ctx, businessID, id)
}

func (s *BillingOpsService) setInvoiceSubscriptionStatus(ctx context.Context, businessID, id, status string) (*models.InvoiceSubscription, error) {
	subscription, err := s.GetInvoiceSubscription(ctx, businessID, id)
	if err != nil {
		return nil, err
	}
	subscription.Status = status
	if status == models.InvoiceSubscriptionStatusPaused {
		subscription.NextRunAt = nil
	}
	if status == models.InvoiceSubscriptionStatusActive && subscription.NextRunAt == nil {
		nextRun := time.Now().UTC()
		subscription.NextRunAt = &nextRun
	}
	if err := s.db.WithContext(ctx).Save(subscription).Error; err != nil {
		return nil, err
	}
	_ = recordActivityLog(ctx, s.db, businessID, "invoice_subscription", id, status, "", subscription, nil, nil)
	return subscription, nil
}

func (s *BillingOpsService) PauseInvoiceSubscription(ctx context.Context, businessID, id string) (*models.InvoiceSubscription, error) {
	return s.setInvoiceSubscriptionStatus(ctx, businessID, id, models.InvoiceSubscriptionStatusPaused)
}

func (s *BillingOpsService) ResumeInvoiceSubscription(ctx context.Context, businessID, id string) (*models.InvoiceSubscription, error) {
	return s.setInvoiceSubscriptionStatus(ctx, businessID, id, models.InvoiceSubscriptionStatusActive)
}

func (s *BillingOpsService) ListInvoiceSubscriptionRuns(ctx context.Context, businessID, subscriptionID string, page, limit int) ([]*models.InvoiceSubscriptionRun, int64, error) {
	if s.db == nil {
		return nil, 0, fmt.Errorf("database is not configured")
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	query := s.db.WithContext(ctx).Model(&models.InvoiceSubscriptionRun{}).
		Where("business_id = ? AND subscription_id = ? AND deleted_at IS NULL", businessID, subscriptionID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []models.InvoiceSubscriptionRun
	if err := query.Order("scheduled_for DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	result := make([]*models.InvoiceSubscriptionRun, 0, len(rows))
	for i := range rows {
		result = append(result, &rows[i])
	}
	return result, total, nil
}

func (s *BillingOpsService) ListAggregatedInvoiceSubscriptionRuns(ctx context.Context, businessID string, page, limit int) ([]*InvoiceSubscriptionRunFeedItem, int64, *InvoiceSubscriptionRunStats, error) {
	if s.db == nil {
		return nil, 0, nil, fmt.Errorf("database is not configured")
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}

	query := s.db.WithContext(ctx).
		Table("invoice_subscription_runs AS runs").
		Joins("JOIN invoice_subscriptions AS subs ON subs.id = runs.subscription_id AND subs.deleted_at IS NULL").
		Where("runs.business_id = ? AND runs.deleted_at IS NULL", businessID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, nil, err
	}

	var rows []InvoiceSubscriptionRunFeedItem
	if err := query.
		Select(`
			runs.id,
			runs.subscription_id,
			subs.name AS subscription_name,
			subs.status AS subscription_status,
			subs.cadence AS subscription_cadence,
			runs.status,
			runs.scheduled_for,
			runs.attempt_count,
			runs.last_error,
			runs.invoice_id,
			runs.document_id,
			runs.created_at,
			runs.completed_at,
			subs.next_run_at,
			subs.last_run_at
		`).
		Order("runs.scheduled_for DESC, runs.created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, 0, nil, err
	}

	stats := &InvoiceSubscriptionRunStats{}
	var statusRows []struct {
		Status string
		Total  int64
	}
	if err := s.db.WithContext(ctx).
		Model(&models.InvoiceSubscription{}).
		Select("status, COUNT(*) AS total").
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Group("status").
		Scan(&statusRows).Error; err != nil {
		return nil, 0, nil, err
	}
	for _, row := range statusRows {
		switch row.Status {
		case models.InvoiceSubscriptionStatusActive:
			stats.ActiveCount = row.Total
		case models.InvoiceSubscriptionStatusPaused:
			stats.PausedCount = row.Total
		}
	}
	stats.TotalRuns = total
	if err := s.db.WithContext(ctx).
		Model(&models.InvoiceSubscriptionRun{}).
		Where("business_id = ? AND status = ? AND deleted_at IS NULL", businessID, "failed").
		Count(&stats.FailedRuns).Error; err != nil {
		return nil, 0, nil, err
	}

	result := make([]*InvoiceSubscriptionRunFeedItem, 0, len(rows))
	for i := range rows {
		result = append(result, &rows[i])
	}
	return result, total, stats, nil
}

func (s *BillingOpsService) decoratePartyGroups(ctx context.Context, businessID string, groups []*models.PartyGroup) error {
	for _, group := range groups {
		summary, err := s.buildPartyGroupLedgerSummary(ctx, businessID, group)
		if err != nil {
			return err
		}
		group.MemberCount = len(group.Members)
		group.InvoiceCount = summary.InvoiceCount
		group.DocumentCount = summary.DocumentCount
		group.ReceivableTotal = summary.ReceivableTotal
		group.PayableTotal = summary.PayableTotal
		group.NetBalance = summary.NetBalance
	}
	return nil
}

func (s *BillingOpsService) buildPartyGroupLedgerSummary(ctx context.Context, businessID string, group *models.PartyGroup) (*PartyGroupLedgerSummary, error) {
	partyByType := map[string][]string{}
	for _, member := range group.Members {
		partyByType[member.PartyType] = append(partyByType[member.PartyType], member.PartyID)
	}
	summary := &PartyGroupLedgerSummary{GroupID: group.ID, Members: group.Members}
	if len(partyByType[models.DocumentPartyTypeCustomer]) > 0 {
		ids := partyByType[models.DocumentPartyTypeCustomer]
		_ = s.db.WithContext(ctx).Model(&models.Invoice{}).
			Where("business_id = ? AND customer_id IN ? AND deleted_at IS NULL", businessID, ids).
			Count(&summary.InvoiceCount).Error
		type totals struct {
			Total   float64
			Paid    float64
			Balance float64
		}
		var invoiceTotals totals
		if err := s.db.WithContext(ctx).Model(&models.Invoice{}).
			Select("COALESCE(SUM(total),0) AS total, COALESCE(SUM(paid_amount),0) AS paid, COALESCE(SUM(balance_due),0) AS balance").
			Where("business_id = ? AND customer_id IN ? AND deleted_at IS NULL", businessID, ids).
			Scan(&invoiceTotals).Error; err == nil {
			summary.TotalAmount += invoiceTotals.Total
			summary.PaidAmount += invoiceTotals.Paid
			summary.BalanceDue += invoiceTotals.Balance
			summary.ReceivableTotal += invoiceTotals.Balance
		}
		var documentCount int64
		_ = s.db.WithContext(ctx).Model(&models.Document{}).
			Where("business_id = ? AND party_type = ? AND party_id IN ? AND deleted_at IS NULL", businessID, models.DocumentPartyTypeCustomer, ids).
			Count(&documentCount).Error
		summary.DocumentCount += documentCount
	}
	if len(partyByType[models.DocumentPartyTypeVendor]) > 0 {
		ids := partyByType[models.DocumentPartyTypeVendor]
		var documentCount int64
		_ = s.db.WithContext(ctx).Model(&models.Document{}).
			Where("business_id = ? AND party_type = ? AND party_id IN ? AND deleted_at IS NULL", businessID, models.DocumentPartyTypeVendor, ids).
			Count(&documentCount).Error
		summary.DocumentCount += documentCount
		type totals struct {
			Total   float64
			Paid    float64
			Balance float64
		}
		var documentTotals totals
		if err := s.db.WithContext(ctx).Model(&models.Document{}).
			Select("COALESCE(SUM(total),0) AS total, COALESCE(SUM(paid_amount),0) AS paid, COALESCE(SUM(balance_due),0) AS balance").
			Where("business_id = ? AND party_type = ? AND party_id IN ? AND deleted_at IS NULL", businessID, models.DocumentPartyTypeVendor, ids).
			Scan(&documentTotals).Error; err == nil {
			summary.TotalAmount += documentTotals.Total
			summary.PaidAmount += documentTotals.Paid
			summary.BalanceDue += documentTotals.Balance
			summary.PayableTotal += documentTotals.Balance
		}
	}
	summary.TransactionCount = summary.InvoiceCount + summary.DocumentCount
	summary.NetBalance = summary.ReceivableTotal - summary.PayableTotal
	return summary, nil
}

func (s *BillingOpsService) decorateInvoiceSubscriptions(ctx context.Context, businessID string, subscriptions []*models.InvoiceSubscription) error {
	if len(subscriptions) == 0 {
		return nil
	}
	subscriptionIDs := make([]string, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		subscriptionIDs = append(subscriptionIDs, subscription.ID)
	}

	var runCounts []struct {
		SubscriptionID string
		Total          int64
	}
	if err := s.db.WithContext(ctx).
		Model(&models.InvoiceSubscriptionRun{}).
		Select("subscription_id, COUNT(*) AS total").
		Where("business_id = ? AND subscription_id IN ? AND deleted_at IS NULL", businessID, subscriptionIDs).
		Group("subscription_id").
		Scan(&runCounts).Error; err != nil {
		return err
	}
	runCountBySubscription := map[string]int64{}
	for _, row := range runCounts {
		runCountBySubscription[row.SubscriptionID] = row.Total
	}

	type latestRunRow struct {
		SubscriptionID string
		Status         string
	}
	subquery := s.db.WithContext(ctx).
		Model(&models.InvoiceSubscriptionRun{}).
		Select("subscription_id, MAX(scheduled_for) AS latest_scheduled_for").
		Where("business_id = ? AND subscription_id IN ? AND deleted_at IS NULL", businessID, subscriptionIDs).
		Group("subscription_id")
	var latestRuns []latestRunRow
	if err := s.db.WithContext(ctx).
		Table("invoice_subscription_runs AS runs").
		Select("runs.subscription_id, runs.status").
		Joins("JOIN (?) AS latest ON latest.subscription_id = runs.subscription_id AND latest.latest_scheduled_for = runs.scheduled_for", subquery).
		Where("runs.business_id = ? AND runs.subscription_id IN ? AND runs.deleted_at IS NULL", businessID, subscriptionIDs).
		Scan(&latestRuns).Error; err != nil {
		return err
	}
	lastRunStatusBySubscription := map[string]string{}
	for _, row := range latestRuns {
		if _, exists := lastRunStatusBySubscription[row.SubscriptionID]; !exists {
			lastRunStatusBySubscription[row.SubscriptionID] = row.Status
		}
	}

	for _, subscription := range subscriptions {
		subscription.RunCount = runCountBySubscription[subscription.ID]
		subscription.LastRunStatus = lastRunStatusBySubscription[subscription.ID]
	}
	return nil
}

func (s *BillingOpsService) DispatchDueInvoiceSubscriptions(ctx context.Context, limit int) ([]*models.InvoiceSubscriptionRun, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	now := time.Now().UTC()
	var subscriptions []models.InvoiceSubscription
	if err := s.db.WithContext(ctx).
		Where("status = ? AND next_run_at IS NOT NULL AND next_run_at <= ? AND deleted_at IS NULL", models.InvoiceSubscriptionStatusActive, now).
		Order("next_run_at ASC").
		Limit(limit).
		Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	for i := range subscriptions {
		if err := rejectInvoiceSubscriptionAutoSend(subscriptions[i].AutoSend); err != nil {
			return nil, fmt.Errorf("dispatch invoice subscription %s: %w", subscriptions[i].ID, err)
		}
	}
	dispatched := make([]*models.InvoiceSubscriptionRun, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		nextRunAt, err := cadenceNextRun(now, subscription.Cadence, subscription.Timezone)
		if err != nil {
			continue
		}
		run := &models.InvoiceSubscriptionRun{
			BusinessID:     subscription.BusinessID,
			SubscriptionID: subscription.ID,
			ScheduledFor:   now,
			Status:         models.BulkJobStatusQueued,
			IdempotencyKey: fmt.Sprintf("dispatch:%s:%d", subscription.ID, now.UnixNano()),
			AttemptCount:   0,
		}
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(run).Error; err != nil {
				return err
			}
			return tx.Model(&models.InvoiceSubscription{}).
				Where("id = ?", subscription.ID).
				Updates(map[string]interface{}{
					"last_run_at": now,
					"next_run_at": nextRunAt,
				}).Error
		}); err != nil {
			return nil, err
		}
		dispatched = append(dispatched, run)
	}
	return dispatched, nil
}

func (s *BillingOpsService) GenerateInvoiceSubscriptionNow(ctx context.Context, businessID, subscriptionID, idempotencyKey string) (*models.InvoiceSubscriptionRun, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if s.invoices == nil {
		return nil, fmt.Errorf("canonical invoice creator is not configured")
	}
	runID, storedKey, err := invoiceSubscriptionRunIdentity(businessID, subscriptionID, idempotencyKey)
	if err != nil {
		return nil, err
	}
	var (
		run           models.InvoiceSubscriptionRun
		subscription  models.InvoiceSubscription
		generationErr error
		generated     bool
	)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Preload("Lines").
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", subscriptionID, businessID).
			First(&subscription).Error; err != nil {
			return err
		}
		scheduledFor := time.Now().UTC()
		run = models.InvoiceSubscriptionRun{
			ID: runID, BusinessID: businessID, SubscriptionID: subscription.ID,
			ScheduledFor: scheduledFor, Status: models.BulkJobStatusProcessing,
			IdempotencyKey: storedKey,
		}
		claim := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&run)
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("business_id = ? AND subscription_id = ? AND idempotency_key = ? AND deleted_at IS NULL", businessID, subscriptionID, storedKey).
				First(&run).Error; err != nil {
				return err
			}
			if run.Status == models.BulkJobStatusCompleted && run.InvoiceID != nil && strings.TrimSpace(*run.InvoiceID) != "" {
				return nil
			}
		}
		if err := validateInvoiceSubscriptionGeneration(&subscription); err != nil {
			return err
		}
		run.Status = models.BulkJobStatusProcessing
		run.AttemptCount++
		run.LastError = nil
		if err := tx.Save(&run).Error; err != nil {
			return err
		}
		input := CreateInvoiceInput{
			BusinessID:           businessID,
			CustomerID:           subscription.CustomerID,
			IdempotencyKey:       run.ID,
			DueDate:              run.ScheduledFor.AddDate(0, 0, 30),
			Notes:                subscription.Notes,
			Items:                invoiceSubscriptionCreateItems(&subscription),
			PriceListID:          pointerStringValue(subscription.PriceListID),
			OriginSubscriptionID: subscription.ID,
			OriginRunID:          run.ID,
		}
		invoice, createErr := s.invoices.CreateByBusiness(ctx, businessID, input)
		if createErr != nil {
			lastError := createErr.Error()
			run.Status = models.BulkJobStatusFailed
			run.LastError = &lastError
			generationErr = createErr
			return tx.Save(&run).Error
		}
		nextRunAt, cadenceErr := cadenceNextRun(run.ScheduledFor, subscription.Cadence, subscription.Timezone)
		if cadenceErr != nil {
			return cadenceErr
		}
		completedAt := time.Now().UTC()
		run.Status = models.BulkJobStatusCompleted
		run.InvoiceID = &invoice.ID
		run.DocumentID = &invoice.ID
		run.CompletedAt = &completedAt
		if err := tx.Save(&run).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.InvoiceSubscription{}).
			Where("id = ? AND business_id = ?", subscription.ID, businessID).
			Updates(map[string]interface{}{
				"last_run_at": completedAt,
				"next_run_at": nextRunAt,
			}).Error; err != nil {
			return err
		}
		generated = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if generationErr != nil {
		return nil, generationErr
	}
	if generated {
		_ = recordActivityLog(ctx, s.db, businessID, "invoice_subscription", subscription.ID, "generated", "", &run, nil, map[string]interface{}{"invoice_id": pointerStringValue(run.InvoiceID)})
	}
	return &run, nil
}

func invoiceSubscriptionRunIdentity(businessID, subscriptionID, idempotencyKey string) (string, string, error) {
	key := strings.TrimSpace(idempotencyKey)
	if _, err := uuid.Parse(key); err != nil {
		return "", "", &idempotency.InvalidKeyError{}
	}
	material := strings.Join([]string{"invoice-subscription-run", strings.TrimSpace(businessID), strings.TrimSpace(subscriptionID), key}, ":")
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(material)).String(), material, nil
}

func invoiceSubscriptionCreateItems(subscription *models.InvoiceSubscription) []CreateInvoiceItemInput {
	if subscription == nil {
		return nil
	}
	items := make([]CreateInvoiceItemInput, 0, len(subscription.Lines))
	for _, line := range subscription.Lines {
		if line == nil {
			continue
		}
		unitPrice := line.UnitPrice
		mrp := line.MRP
		cessRate := line.CessRate
		if subscription.PricePolicy == models.InvoiceSubscriptionPricePolicyFollow {
			unitPrice = 0
			mrp = 0
			cessRate = 0
		}
		items = append(items, CreateInvoiceItemInput{
			ProductID:      pointerStringValue(line.ProductID),
			VariantID:      pointerStringValue(line.VariantID),
			WarehouseID:    pointerStringValue(line.WarehouseID),
			Description:    line.Description,
			Quantity:       line.Quantity,
			FreeQuantity:   line.FreeQuantity,
			UnitPrice:      unitPrice,
			MRP:            mrp,
			Discount:       line.DiscountAmount,
			TaxRate:        line.TaxRate,
			CessRate:       cessRate,
			CustomFields:   unmarshalJSONMap(line.CustomFields),
			ChargeSnapshot: readMapSliceString(line.AdditionalCharge),
		})
	}
	return items
}

func readMapSliceString(raw string) []map[string]interface{} {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var value []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil
	}
	return value
}
