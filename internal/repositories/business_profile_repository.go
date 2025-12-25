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
	"gorm.io/gorm"
)

// BusinessProfileRepository defines the interface for business profile data operations
type BusinessProfileRepository interface {
	// Core CRUD
	Create(ctx context.Context, profile *models.BusinessProfile) error
	GetByID(ctx context.Context, id string) (*models.BusinessProfile, error)
	Update(ctx context.Context, profile *models.BusinessProfile) error
	Delete(ctx context.Context, id string) error

	// User-specific queries
	GetByUserID(ctx context.Context, userID string) ([]*models.BusinessProfile, error)
	GetDefault(ctx context.Context, userID string) (*models.BusinessProfile, error)
	HasAny(ctx context.Context, userID string) (bool, error)

	// Specific operations
	GetByGSTIN(ctx context.Context, gstin string) (*models.BusinessProfile, error)
	UpdateLogoURL(ctx context.Context, id, logoURL string) error
	SetDefault(ctx context.Context, id, userID string) error
	IncrementInvoiceSequence(ctx context.Context, id string) (int, error)

	// Search
	SearchByName(ctx context.Context, userID, name string) ([]*models.BusinessProfile, error)
}

type businessProfileRepository struct {
	db    *database.Database
	cache *businessProfileCacheRepository
}

// businessProfileCacheRepository handles DynamoDB caching for business profiles
type businessProfileCacheRepository struct {
	client    *awsclients.DynamoDBClient
	tableName string
	ttl       time.Duration
}

// NewBusinessProfileCacheRepository creates a new business profile cache repository
func NewBusinessProfileCacheRepository(client *awsclients.DynamoDBClient, tableName string, ttl time.Duration) *businessProfileCacheRepository {
	return &businessProfileCacheRepository{
		client:    client,
		tableName: tableName,
		ttl:       ttl,
	}
}

// Get retrieves a business profile from cache
func (c *businessProfileCacheRepository) Get(ctx context.Context, userID, profileID string) (*models.BusinessProfileCache, error) {
	input := &dynamodb.GetItemInput{
		TableName: aws.String(c.tableName),
		Key: map[string]types.AttributeValue{
			"user_id":    &types.AttributeValueMemberS{Value: userID},
			"profile_id": &types.AttributeValueMemberS{Value: profileID},
		},
	}

	output, err := c.client.GetItem(ctx, input)
	if err != nil {
		return nil, err
	}

	if output.Item == nil {
		return nil, nil
	}

	var cache models.BusinessProfileCache
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

// Set stores a business profile in cache
func (c *businessProfileCacheRepository) Set(ctx context.Context, profile *models.BusinessProfile) error {
	cache := &models.BusinessProfileCache{
		UserID:              profile.UserID.String(),
		ProfileID:           profile.ID.String(),
		Name:                profile.Name,
		Type:                string(profile.Type),
		Address:             profile.Address,
		City:                profile.City,
		State:               profile.State,
		StateCode:           profile.StateCode,
		Pincode:             profile.Pincode,
		GSTIN:               profile.GSTIN,
		PAN:                 profile.PAN,
		Email:               profile.Email,
		Phone:               profile.Phone,
		Website:             profile.Website,
		LogoURL:             profile.LogoURL,
		InvoicePrefix:       profile.InvoicePrefix,
		InvoiceStartingNo:   profile.InvoiceStartingNo,
		GSTReturnFrequency:  profile.GSTReturnFrequency,
		IsDefault:           profile.IsDefault,
		CreatedAt:           profile.CreatedAt,
		CachedAt:            time.Now(),
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

// Delete removes a business profile from cache
func (c *businessProfileCacheRepository) Delete(ctx context.Context, userID, profileID string) error {
	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(c.tableName),
		Key: map[string]types.AttributeValue{
			"user_id":    &types.AttributeValueMemberS{Value: userID},
			"profile_id": &types.AttributeValueMemberS{Value: profileID},
		},
	}

	_, err := c.client.DeleteItem(ctx, input)
	return err
}

// InvalidateByUser invalidates all profiles for a user
func (c *businessProfileCacheRepository) InvalidateByUser(ctx context.Context, userID string) error {
	input := &dynamodb.ScanInput{
		TableName: aws.String(c.tableName),
		FilterExpression: aws.String("user_id = :user_id"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":user_id": &types.AttributeValueMemberS{Value: userID},
		},
	}

	output, err := c.client.Scan(ctx, input)
	if err != nil {
		return err
	}

	for _, item := range output.Items {
		var userID, profileID string
		for k, v := range item {
			if k == "user_id" {
				if s, ok := v.(*types.AttributeValueMemberS); ok {
					userID = s.Value
				}
			}
			if k == "profile_id" {
				if s, ok := v.(*types.AttributeValueMemberS); ok {
					profileID = s.Value
				}
			}
		}
		if userID != "" && profileID != "" {
			c.Delete(ctx, userID, profileID)
		}
	}

	return nil
}

// NewBusinessProfileRepository creates a new business profile repository
func NewBusinessProfileRepository(db *database.Database, cache *businessProfileCacheRepository) BusinessProfileRepository {
	return &businessProfileRepository{
		db:    db,
		cache: cache,
	}
}

func (r *businessProfileRepository) Create(ctx context.Context, profile *models.BusinessProfile) error {
	err := r.db.DB.WithContext(ctx).Create(profile).Error
	if err != nil {
		return err
	}

	// Update cache asynchronously
	if r.cache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r.cache.Set(cacheCtx, profile)
		}()
	}

	return nil
}

func (r *businessProfileRepository) GetByID(ctx context.Context, id string) (*models.BusinessProfile, error) {
	var profile models.BusinessProfile
	err := r.db.DB.WithContext(ctx).Where("id = ?", id).First(&profile).Error
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
			r.cache.Set(cacheCtx, &profile)
		}()
	}

	return &profile, nil
}

func (r *businessProfileRepository) Update(ctx context.Context, profile *models.BusinessProfile) error {
	err := r.db.DB.WithContext(ctx).Save(profile).Error
	if err != nil {
		return err
	}

	// Update cache
	if r.cache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r.cache.Set(cacheCtx, profile)
		}()
	}

	return nil
}

func (r *businessProfileRepository) Delete(ctx context.Context, id string) error {
	profile, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if profile == nil {
		return nil
	}

	err = r.db.DB.WithContext(ctx).Delete(&models.BusinessProfile{}, "id = ?", id).Error
	if err != nil {
		return err
	}

	// Invalidate cache
	if r.cache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r.cache.Delete(cacheCtx, profile.UserID.String(), profile.ID.String())
		}()
	}

	return nil
}

func (r *businessProfileRepository) GetByUserID(ctx context.Context, userID string) ([]*models.BusinessProfile, error) {
	var profiles []*models.BusinessProfile
	err := r.db.DB.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("is_default DESC, created_at ASC").
		Find(&profiles).Error
	return profiles, err
}

func (r *businessProfileRepository) GetDefault(ctx context.Context, userID string) (*models.BusinessProfile, error) {
	var profile models.BusinessProfile
	err := r.db.DB.WithContext(ctx).
		Where("user_id = ? AND is_default = ?", userID, true).
		First(&profile).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// If no default, return the first profile
			err := r.db.DB.WithContext(ctx).
				Where("user_id = ?", userID).
				Order("created_at ASC").
				First(&profile).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, nil
				}
				return nil, err
			}
			return &profile, nil
		}
		return nil, err
	}
	return &profile, nil
}

func (r *businessProfileRepository) HasAny(ctx context.Context, userID string) (bool, error) {
	var count int64
	err := r.db.DB.WithContext(ctx).
		Model(&models.BusinessProfile{}).
		Where("user_id = ?", userID).
		Count(&count).Error
	return count > 0, err
}

func (r *businessProfileRepository) GetByGSTIN(ctx context.Context, gstin string) (*models.BusinessProfile, error) {
	var profile models.BusinessProfile
	err := r.db.DB.WithContext(ctx).Where("gstin = ?", gstin).First(&profile).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &profile, nil
}

func (r *businessProfileRepository) UpdateLogoURL(ctx context.Context, id, logoURL string) error {
	return r.db.DB.WithContext(ctx).
		Model(&models.BusinessProfile{}).
		Where("id = ?", id).
		Update("logo_url", logoURL).Error
}

func (r *businessProfileRepository) SetDefault(ctx context.Context, id, userID string) error {
	return r.db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Unset all defaults for this user
		if err := tx.Model(&models.BusinessProfile{}).
			Where("user_id = ? AND id != ?", userID, id).
			Update("is_default", false).Error; err != nil {
			return err
		}

		// Set this one as default
		if err := tx.Model(&models.BusinessProfile{}).
			Where("id = ?", id).
			Update("is_default", true).Error; err != nil {
			return err
		}

		// Invalidate cache
		if r.cache != nil {
			go func() {
				cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				r.cache.InvalidateByUser(cacheCtx, userID)
			}()
		}

		return nil
	})
}

func (r *businessProfileRepository) IncrementInvoiceSequence(ctx context.Context, id string) (int, error) {
	var profile models.BusinessProfile
	if err := r.db.DB.WithContext(ctx).Where("id = ?", id).First(&profile).Error; err != nil {
		return 0, err
	}

	profile.IncrementInvoiceSequence()
	if err := r.db.DB.WithContext(ctx).Save(&profile).Error; err != nil {
		return 0, err
	}

	// Invalidate cache
	if r.cache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r.cache.Delete(cacheCtx, profile.UserID.String(), profile.ID.String())
		}()
	}

	return profile.InvoiceStartingNo, nil
}

func (r *businessProfileRepository) SearchByName(ctx context.Context, userID, name string) ([]*models.BusinessProfile, error) {
	var profiles []*models.BusinessProfile
	searchPattern := "%" + name + "%"

	err := r.db.DB.WithContext(ctx).
		Where("user_id = ? AND name ILIKE ?", userID, searchPattern).
		Order("name ASC").
		Find(&profiles).Error

	return profiles, err
}
