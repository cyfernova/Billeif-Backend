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
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// CustomerRepository defines the interface for customer data operations
type CustomerRepository interface {
	// Core CRUD
	Create(ctx context.Context, customer *models.Customer) error
	GetByID(ctx context.Context, id string) (*models.Customer, error)
	Update(ctx context.Context, customer *models.Customer) error
	Delete(ctx context.Context, id string) error

	// Business-specific queries
	GetByBusinessID(ctx context.Context, businessID string, page, perPage int) ([]*models.Customer, int64, error)
	GetByGSTIN(ctx context.Context, gstin string) (*models.Customer, error)

	// Search and filter
	SearchByName(ctx context.Context, businessID, name string, page, perPage int) ([]*models.Customer, int64, error)
	FilterByType(ctx context.Context, businessID string, customerType models.CustomerType, page, perPage int) ([]*models.Customer, int64, error)

	// Balance operations
	UpdateBalance(ctx context.Context, id string, amount decimal.Decimal, transactionType, description string, referenceID *string, referenceType string) error
	GetOutstanding(ctx context.Context, businessID string) ([]*models.Customer, error)

	// Statistics
	GetStats(ctx context.Context, businessID string) (*CustomerStats, error)

	// Duplicate check
	CheckDuplicate(ctx context.Context, businessID string, gstin, pan, email string) (bool, error)
}

// CustomerStats represents customer statistics
type CustomerStats struct {
	TotalCustomers   int64           `json:"total_customers"`
	ActiveCustomers  int64           `json:"active_customers"`
	InactiveCustomers int64          `json:"inactive_customers"`
	TotalOutstanding decimal.Decimal `json:"total_outstanding"`
	AverageBalance   decimal.Decimal `json:"average_balance"`
	CountByType      map[string]int64 `json:"count_by_type"`
}

type customerRepository struct {
	db    *database.Database
	cache *customerCacheRepository
}

// customerCacheRepository handles DynamoDB caching for customers
type customerCacheRepository struct {
	client    *awsclients.DynamoDBClient
	tableName string
	ttl       time.Duration
}

// NewCustomerCacheRepository creates a new customer cache repository
func NewCustomerCacheRepository(client *awsclients.DynamoDBClient, tableName string, ttl time.Duration) *customerCacheRepository {
	return &customerCacheRepository{
		client:    client,
		tableName: tableName,
		ttl:       ttl,
	}
}

// Get retrieves a customer from cache
func (c *customerCacheRepository) Get(ctx context.Context, businessID, customerID string) (*models.CustomerCache, error) {
	input := &dynamodb.GetItemInput{
		TableName: aws.String(c.tableName),
		Key: map[string]types.AttributeValue{
			"business_id": &types.AttributeValueMemberS{Value: businessID},
			"customer_id": &types.AttributeValueMemberS{Value: customerID},
		},
	}

	output, err := c.client.GetItem(ctx, input)
	if err != nil {
		return nil, err
	}

	if output.Item == nil {
		return nil, nil
	}

	var cache models.CustomerCache
	err = attributevalue.UnmarshalMap(output.Item, &cache)
	if err != nil {
		return nil, err
	}

	// Check if expired
	if time.Now().Unix() > cache.ExpiresAt {
		return nil, nil // Cache expired
	}

	return &cache, nil
}

// Set stores a customer in cache
func (c *customerCacheRepository) Set(ctx context.Context, customer *models.Customer) error {
	cache := &models.CustomerCache{
		BusinessID:    customer.BusinessID.String(),
		CustomerID:    customer.ID.String(),
		Name:          customer.Name,
		Type:          string(customer.Type),
		Phone:         customer.Phone,
		Email:         customer.Email,
		Address:       customer.Address,
		City:          customer.City,
		State:         customer.State,
		Pincode:       customer.Pincode,
		GSTIN:         customer.GSTIN,
		PAN:           customer.PAN,
		CreditLimit:   customer.CreditLimit.InexactFloat64(),
		CreditPeriod:  customer.CreditPeriod,
		Balance:       customer.Balance.InexactFloat64(),
		IsActive:      customer.IsActive,
		CreatedBy:     customer.CreatedBy.String(),
		CreatedAt:     customer.CreatedAt,
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

// Delete removes a customer from cache
func (c *customerCacheRepository) Delete(ctx context.Context, businessID, customerID string) error {
	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(c.tableName),
		Key: map[string]types.AttributeValue{
			"business_id": &types.AttributeValueMemberS{Value: businessID},
			"customer_id": &types.AttributeValueMemberS{Value: customerID},
		},
	}

	_, err := c.client.DeleteItem(ctx, input)
	return err
}

// InvalidateByBusiness invalidates all customers for a business
func (c *customerCacheRepository) InvalidateByBusiness(ctx context.Context, businessID string) error {
	// Scan and delete all items for this business
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

	// Batch delete
	for _, item := range output.Items {
		var businessID, customerID string
		for k, v := range item {
			if k == "business_id" {
				if s, ok := v.(*types.AttributeValueMemberS); ok {
					businessID = s.Value
				}
			}
			if k == "customer_id" {
				if s, ok := v.(*types.AttributeValueMemberS); ok {
					customerID = s.Value
				}
			}
		}
		if businessID != "" && customerID != "" {
			c.Delete(ctx, businessID, customerID)
		}
	}

	return nil
}

// NewCustomerRepository creates a new customer repository
func NewCustomerRepository(db *database.Database, cache *customerCacheRepository) CustomerRepository {
	return &customerRepository{
		db:    db,
		cache: cache,
	}
}

func (r *customerRepository) Create(ctx context.Context, customer *models.Customer) error {
	err := r.db.DB.WithContext(ctx).Create(customer).Error
	if err != nil {
		return err
	}

	// Update cache asynchronously
	if r.cache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r.cache.Set(cacheCtx, customer)
		}()
	}

	return nil
}

func (r *customerRepository) GetByID(ctx context.Context, id string) (*models.Customer, error) {
	// Try cache first
	if r.cache != nil {
		// We need business_id for cache lookup, skip for now
	}

	var customer models.Customer
	err := r.db.DB.WithContext(ctx).Where("id = ?", id).First(&customer).Error
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
			r.cache.Set(cacheCtx, &customer)
		}()
	}

	return &customer, nil
}

func (r *customerRepository) Update(ctx context.Context, customer *models.Customer) error {
	err := r.db.DB.WithContext(ctx).Save(customer).Error
	if err != nil {
		return err
	}

	// Update cache
	if r.cache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r.cache.Set(cacheCtx, customer)
		}()
	}

	return nil
}

func (r *customerRepository) Delete(ctx context.Context, id string) error {
	// Get customer first for cache invalidation
	customer, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if customer == nil {
		return nil // Not found, nothing to delete
	}

	err = r.db.DB.WithContext(ctx).Delete(&models.Customer{}, "id = ?", id).Error
	if err != nil {
		return err
	}

	// Invalidate cache
	if r.cache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r.cache.Delete(cacheCtx, customer.BusinessID.String(), customer.ID.String())
		}()
	}

	return nil
}

func (r *customerRepository) GetByBusinessID(ctx context.Context, businessID string, page, perPage int) ([]*models.Customer, int64, error) {
	var customers []*models.Customer
	var total int64

	offset := (page - 1) * perPage

	query := r.db.DB.WithContext(ctx).Model(&models.Customer{}).Where("business_id = ?", businessID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.
		Offset(offset).
		Limit(perPage).
		Order("created_at DESC").
		Find(&customers).Error

	return customers, total, err
}

func (r *customerRepository) GetByGSTIN(ctx context.Context, gstin string) (*models.Customer, error) {
	var customer models.Customer
	err := r.db.DB.WithContext(ctx).Where("gstin = ?", gstin).First(&customer).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &customer, nil
}

func (r *customerRepository) SearchByName(ctx context.Context, businessID, name string, page, perPage int) ([]*models.Customer, int64, error) {
	var customers []*models.Customer
	var total int64

	offset := (page - 1) * perPage
	searchPattern := "%" + name + "%"

	query := r.db.DB.WithContext(ctx).Model(&models.Customer{}).
		Where("business_id = ? AND name ILIKE ?", businessID, searchPattern)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.
		Offset(offset).
		Limit(perPage).
		Order("name ASC").
		Find(&customers).Error

	return customers, total, err
}

func (r *customerRepository) FilterByType(ctx context.Context, businessID string, customerType models.CustomerType, page, perPage int) ([]*models.Customer, int64, error) {
	var customers []*models.Customer
	var total int64

	offset := (page - 1) * perPage

	query := r.db.DB.WithContext(ctx).Model(&models.Customer{}).
		Where("business_id = ? AND type = ?", businessID, customerType)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.
		Offset(offset).
		Limit(perPage).
		Order("name ASC").
		Find(&customers).Error

	return customers, total, err
}

func (r *customerRepository) UpdateBalance(ctx context.Context, id string, amount decimal.Decimal, transactionType, description string, referenceID *string, referenceType string) error {
	return r.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Get current customer
		var customer models.Customer
		if err := tx.Where("id = ?", id).First(&customer).Error; err != nil {
			return err
		}

		balanceBefore := customer.Balance
		balanceAfter := balanceBefore.Add(amount)

		// Create transaction record
		transaction := &models.CustomerBalanceTransaction{
			CustomerID:      customer.ID,
			Amount:          amount,
			BalanceBefore:   balanceBefore,
			BalanceAfter:    balanceAfter,
			TransactionType: transactionType,
			Description:     description,
			ReferenceType:   referenceType,
		}

		if referenceID != nil {
			// Parse UUID
			if refID, err := parseUUID(*referenceID); err == nil {
				transaction.ReferenceID = &refID
			}
		}

		if err := tx.Create(transaction).Error; err != nil {
			return err
		}

		// Update customer balance
		if err := tx.Model(&customer).Update("balance", balanceAfter).Error; err != nil {
			return err
		}

		// Invalidate cache
		if r.cache != nil {
			go func() {
				cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				r.cache.Delete(cacheCtx, customer.BusinessID.String(), customer.ID.String())
			}()
		}

		return nil
	})
}

func (r *customerRepository) GetOutstanding(ctx context.Context, businessID string) ([]*models.Customer, error) {
	var customers []*models.Customer
	err := r.db.DB.WithContext(ctx).
		Where("business_id = ? AND balance != 0 AND is_active = ?", businessID, true).
		Order("balance DESC").
		Find(&customers).Error
	return customers, err
}

func (r *customerRepository) GetStats(ctx context.Context, businessID string) (*CustomerStats, error) {
	var stats CustomerStats

	// Total customers
	if err := r.db.DB.WithContext(ctx).Model(&models.Customer{}).
		Where("business_id = ?", businessID).
		Count(&stats.TotalCustomers).Error; err != nil {
		return nil, err
	}

	// Active customers
	if err := r.db.DB.WithContext(ctx).Model(&models.Customer{}).
		Where("business_id = ? AND is_active = ?", businessID, true).
		Count(&stats.ActiveCustomers).Error; err != nil {
		return nil, err
	}

	// Inactive customers
	stats.InactiveCustomers = stats.TotalCustomers - stats.ActiveCustomers

	// Total outstanding
	var outstanding decimal.Decimal
	if err := r.db.DB.WithContext(ctx).Model(&models.Customer{}).
		Where("business_id = ? AND is_active = ?", businessID, true).
		Select("COALESCE(SUM(ABS(balance)), 0)").
		Scan(&outstanding).Error; err != nil {
		return nil, err
	}
	stats.TotalOutstanding = outstanding

	// Average balance
	if stats.ActiveCustomers > 0 {
		stats.AverageBalance = outstanding.Div(decimal.NewFromInt(stats.ActiveCustomers))
	}

	// Count by type
	type CountResult struct {
		Type  string
		Count int64
	}
	var results []CountResult
	if err := r.db.DB.WithContext(ctx).Model(&models.Customer{}).
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

func (r *customerRepository) CheckDuplicate(ctx context.Context, businessID string, gstin, pan, email string) (bool, error) {
	var count int64

	query := r.db.DB.WithContext(ctx).Model(&models.Customer{}).
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

// parseUUID parses a UUID string
func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}
