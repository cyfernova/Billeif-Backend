package repositories

import (
	"context"
	"errors"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/cyfernova/invoice-backend/internal/database"
	"github.com/cyfernova/invoice-backend/internal/models"
	"github.com/cyfernova/invoice-backend/pkg/awsclients"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// VendorRepository defines the interface for vendor data operations
type VendorRepository interface {
	// Core CRUD
	Create(ctx context.Context, vendor *models.Vendor) error
	GetByID(ctx context.Context, id string) (*models.Vendor, error)
	Update(ctx context.Context, vendor *models.Vendor) error
	Delete(ctx context.Context, id string) error

	// Business-specific queries
	GetByBusinessID(ctx context.Context, businessID string, page, perPage int) ([]*models.Vendor, int64, error)
	GetByGSTIN(ctx context.Context, gstin string) (*models.Vendor, error)

	// Search and filter
	SearchByName(ctx context.Context, businessID, name string, page, perPage int) ([]*models.Vendor, int64, error)
	FilterByType(ctx context.Context, businessID string, vendorType models.VendorType, page, perPage int) ([]*models.Vendor, int64, error)

	// Balance operations
	UpdateBalance(ctx context.Context, id string, amount decimal.Decimal, transactionType, description string, referenceID *string, referenceType string) error
	GetOutstanding(ctx context.Context, businessID string) ([]*models.Vendor, error)

	// Statistics
	GetStats(ctx context.Context, businessID string) (*VendorStats, error)

	// Duplicate check
	CheckDuplicate(ctx context.Context, businessID string, gstin, pan, email string) (bool, error)
}

// VendorStats represents vendor statistics
type VendorStats struct {
	TotalVendors      int64           `json:"total_vendors"`
	ActiveVendors     int64           `json:"active_vendors"`
	InactiveVendors   int64           `json:"inactive_vendors"`
	TotalOutstanding  decimal.Decimal `json:"total_outstanding"`
	AverageBalance    decimal.Decimal `json:"average_balance"`
	CountByType       map[string]int64 `json:"count_by_type"`
}

type vendorRepository struct {
	db    *database.Database
	cache *vendorCacheRepository
}

// vendorCacheRepository handles DynamoDB caching for vendors
type vendorCacheRepository struct {
	client    *awsclients.DynamoDBClient
	tableName string
	ttl       time.Duration
}

// NewVendorCacheRepository creates a new vendor cache repository
func NewVendorCacheRepository(client *awsclients.DynamoDBClient, tableName string, ttl time.Duration) *vendorCacheRepository {
	return &vendorCacheRepository{
		client:    client,
		tableName: tableName,
		ttl:       ttl,
	}
}

// Get retrieves a vendor from cache
func (c *vendorCacheRepository) Get(ctx context.Context, businessID, vendorID string) (*models.VendorCache, error) {
	input := &dynamodb.GetItemInput{
		TableName: aws.String(c.tableName),
		Key: map[string]types.AttributeValue{
			"business_id": &types.AttributeValueMemberS{Value: businessID},
			"vendor_id":   &types.AttributeValueMemberS{Value: vendorID},
		},
	}

	output, err := c.client.GetItem(ctx, input)
	if err != nil {
		return nil, err
	}

	if output.Item == nil {
		return nil, nil
	}

	var cache models.VendorCache
	err = attributevalue.UnmarshalMap(output.Item, &cache)
	if err != nil {
		return nil, err
	}

	// Check if expired
	if time.Now().Unix() > cache.ExpiresAt {
		return nil, nil
	}

	return &cache, nil
}

// Set stores a vendor in cache
func (c *vendorCacheRepository) Set(ctx context.Context, vendor *models.Vendor) error {
	cache := &models.VendorCache{
		BusinessID:    vendor.BusinessID.String(),
		VendorID:      vendor.ID.String(),
		Name:          vendor.Name,
		Type:          string(vendor.Type),
		Phone:         vendor.Phone,
		Email:         vendor.Email,
		Address:       vendor.Address,
		City:          vendor.City,
		State:         vendor.State,
		Pincode:       vendor.Pincode,
		GSTIN:         vendor.GSTIN,
		PAN:           vendor.PAN,
		CreditLimit:   vendor.CreditLimit.InexactFloat64(),
		CreditPeriod:  vendor.CreditPeriod,
		Balance:       vendor.Balance.InexactFloat64(),
		PaymentTerms:  vendor.PaymentTerms,
		BankAccountNo: vendor.BankAccountNo,
		BankIFSC:      vendor.BankIFSC,
		BankName:      vendor.BankName,
		IsActive:      vendor.IsActive,
		CreatedBy:     vendor.CreatedBy.String(),
		CreatedAt:     vendor.CreatedAt,
		CachedAt:      time.Now(),
	}
	cache.SetTTL(c.ttl)

	item, err := attributevalue.MarshalMap(cache)
	if err != nil {
		return err
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String(c.tableName),
		Item:      item,
	}

	_, err = c.client.PutItem(ctx, input)
	return err
}

// Delete removes a vendor from cache
func (c *vendorCacheRepository) Delete(ctx context.Context, businessID, vendorID string) error {
	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(c.tableName),
		Key: map[string]types.AttributeValue{
			"business_id": &types.AttributeValueMemberS{Value: businessID},
			"vendor_id":   &types.AttributeValueMemberS{Value: vendorID},
		},
	}

	_, err := c.client.DeleteItem(ctx, input)
	return err
}

// InvalidateByBusiness invalidates all vendors for a business
func (c *vendorCacheRepository) InvalidateByBusiness(ctx context.Context, businessID string) error {
	input := &dynamodb.ScanInput{
		TableName: aws.String(c.tableName),
		FilterExpression: aws.String("business_id = :business_id"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":business_id": &types.AttributeValueMemberS{Value: businessID},
		},
	}

	output, err := c.client.Scan(ctx, input)
	if err != nil {
		return err
	}

	for _, item := range output.Items {
		var businessID, vendorID string
		for k, v := range item {
			if k == "business_id" {
				if s, ok := v.(*types.AttributeValueMemberS); ok {
					businessID = s.Value
				}
			}
			if k == "vendor_id" {
				if s, ok := v.(*types.AttributeValueMemberS); ok {
					vendorID = s.Value
				}
			}
		}
		if businessID != "" && vendorID != "" {
			c.Delete(ctx, businessID, vendorID)
		}
	}

	return nil
}

// NewVendorRepository creates a new vendor repository
func NewVendorRepository(db *database.Database, cache *vendorCacheRepository) VendorRepository {
	return &vendorRepository{
		db:    db,
		cache: cache,
	}
}

func (r *vendorRepository) Create(ctx context.Context, vendor *models.Vendor) error {
	err := r.db.DB.WithContext(ctx).Create(vendor).Error
	if err != nil {
		return err
	}

	// Update cache asynchronously
	if r.cache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r.cache.Set(cacheCtx, vendor)
		}()
	}

	return nil
}

func (r *vendorRepository) GetByID(ctx context.Context, id string) (*models.Vendor, error) {
	var vendor models.Vendor
	err := r.db.DB.WithContext(ctx).Where("id = ?", id).First(&vendor).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	// Update cache asynchronously
	if r.cache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r.cache.Set(cacheCtx, &vendor)
		}()
	}

	return &vendor, nil
}

func (r *vendorRepository) Update(ctx context.Context, vendor *models.Vendor) error {
	err := r.db.DB.WithContext(ctx).Save(vendor).Error
	if err != nil {
		return err
	}

	// Update cache
	if r.cache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r.cache.Set(cacheCtx, vendor)
		}()
	}

	return nil
}

func (r *vendorRepository) Delete(ctx context.Context, id string) error {
	vendor, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if vendor == nil {
		return nil
	}

	err = r.db.DB.WithContext(ctx).Delete(&models.Vendor{}, "id = ?", id).Error
	if err != nil {
		return err
	}

	// Invalidate cache
	if r.cache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r.cache.Delete(cacheCtx, vendor.BusinessID.String(), vendor.ID.String())
		}()
	}

	return nil
}

func (r *vendorRepository) GetByBusinessID(ctx context.Context, businessID string, page, perPage int) ([]*models.Vendor, int64, error) {
	var vendors []*models.Vendor
	var total int64

	offset := (page - 1) * perPage

	query := r.db.DB.WithContext(ctx).Model(&models.Vendor{}).Where("business_id = ?", businessID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.
		Offset(offset).
		Limit(perPage).
		Order("created_at DESC").
		Find(&vendors).Error

	return vendors, total, err
}

func (r *vendorRepository) GetByGSTIN(ctx context.Context, gstin string) (*models.Vendor, error) {
	var vendor models.Vendor
	err := r.db.DB.WithContext(ctx).Where("gstin = ?", gstin).First(&vendor).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &vendor, nil
}

func (r *vendorRepository) SearchByName(ctx context.Context, businessID, name string, page, perPage int) ([]*models.Vendor, int64, error) {
	var vendors []*models.Vendor
	var total int64

	offset := (page - 1) * perPage
	searchPattern := "%" + name + "%"

	query := r.db.DB.WithContext(ctx).Model(&models.Vendor{}).
		Where("business_id = ? AND name ILIKE ?", businessID, searchPattern)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.
		Offset(offset).
		Limit(perPage).
		Order("name ASC").
		Find(&vendors).Error

	return vendors, total, err
}

func (r *vendorRepository) FilterByType(ctx context.Context, businessID string, vendorType models.VendorType, page, perPage int) ([]*models.Vendor, int64, error) {
	var vendors []*models.Vendor
	var total int64

	offset := (page - 1) * perPage

	query := r.db.DB.WithContext(ctx).Model(&models.Vendor{}).
		Where("business_id = ? AND type = ?", businessID, vendorType)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.
		Offset(offset).
		Limit(perPage).
		Order("name ASC").
		Find(&vendors).Error

	return vendors, total, err
}

func (r *vendorRepository) UpdateBalance(ctx context.Context, id string, amount decimal.Decimal, transactionType, description string, referenceID *string, referenceType string) error {
	return r.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var vendor models.Vendor
		if err := tx.Where("id = ?", id).First(&vendor).Error; err != nil {
			return err
		}

		balanceBefore := vendor.Balance
		balanceAfter := balanceBefore.Add(amount)

		transaction := &models.VendorBalanceTransaction{
			VendorID:        vendor.ID,
			Amount:          amount,
			BalanceBefore:   balanceBefore,
			BalanceAfter:    balanceAfter,
			TransactionType: transactionType,
			Description:     description,
			ReferenceType:   referenceType,
		}

		if referenceID != nil {
			if refID, err := parseUUID(*referenceID); err == nil {
				transaction.ReferenceID = &refID
			}
		}

		if err := tx.Create(transaction).Error; err != nil {
			return err
		}

		if err := tx.Model(&vendor).Update("balance", balanceAfter).Error; err != nil {
			return err
		}

		// Invalidate cache
		if r.cache != nil {
			go func() {
				cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				r.cache.Delete(cacheCtx, vendor.BusinessID.String(), vendor.ID.String())
			}()
		}

		return nil
	})
}

func (r *vendorRepository) GetOutstanding(ctx context.Context, businessID string) ([]*models.Vendor, error) {
	var vendors []*models.Vendor
	err := r.db.DB.WithContext(ctx).
		Where("business_id = ? AND balance != 0 AND is_active = ?", businessID, true).
		Order("balance DESC").
		Find(&vendors).Error
	return vendors, err
}

func (r *vendorRepository) GetStats(ctx context.Context, businessID string) (*VendorStats, error) {
	var stats VendorStats

	if err := r.db.DB.WithContext(ctx).Model(&models.Vendor{}).
		Where("business_id = ?", businessID).
		Count(&stats.TotalVendors).Error; err != nil {
		return nil, err
	}

	if err := r.db.DB.WithContext(ctx).Model(&models.Vendor{}).
		Where("business_id = ? AND is_active = ?", businessID, true).
		Count(&stats.ActiveVendors).Error; err != nil {
		return nil, err
	}

	stats.InactiveVendors = stats.TotalVendors - stats.ActiveVendors

	var outstanding decimal.Decimal
	if err := r.db.DB.WithContext(ctx).Model(&models.Vendor{}).
		Where("business_id = ? AND is_active = ?", businessID, true).
		Select("COALESCE(SUM(ABS(balance)), 0)").
		Scan(&outstanding).Error; err != nil {
		return nil, err
	}
	stats.TotalOutstanding = outstanding

	if stats.ActiveVendors > 0 {
		stats.AverageBalance = outstanding.Div(decimal.NewFromInt(stats.ActiveVendors))
	}

	type CountResult struct {
		Type  string
		Count int64
	}
	var results []CountResult
	if err := r.db.DB.WithContext(ctx).Model(&models.Vendor{}).
		Where("business_id = ?", businessID).
		Select("type, COUNT(*) as count").
		Group("type").
		Scan(&results).Error; err != nil {
		return nil, err
	}

	stats.CountByType = make(map[string]int64)
	for _, r := range results {
		stats.CountByType[r.Type] = r.Count
	}

	return &stats, nil
}

func (r *vendorRepository) CheckDuplicate(ctx context.Context, businessID string, gstin, pan, email string) (bool, error) {
	var count int64

	query := r.db.DB.WithContext(ctx).Model(&models.Vendor{}).
		Where("business_id = ?", businessID)

	if gstin != "" {
		query = query.Or("gstin = ?", gstin)
	}
	if pan != "" {
		query = query.Or("pan = ?", pan)
	}
	if email != "" {
		query = query.Or("email = ?", email)
	}

	if err := query.Count(&count).Error; err != nil {
		return false, err
	}

	return count > 0, nil
}
