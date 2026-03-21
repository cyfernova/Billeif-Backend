package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type userRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) interfaces.UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) Create(ctx context.Context, user *models.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *userRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("user not found")
	}
	return &user, err
}

func (r *userRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	if email == "" {
		return nil, errors.New("user not found")
	}
	var user models.User
	err := r.db.WithContext(ctx).Where("email = ? AND deleted_at IS NULL", email).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("user not found")
	}
	return &user, err
}

func (r *userRepository) GetByPhoneNumber(ctx context.Context, phoneNumber string) (*models.User, error) {
	if phoneNumber == "" {
		return nil, errors.New("user not found")
	}
	var user models.User
	err := r.db.WithContext(ctx).Where("phone_number = ? AND deleted_at IS NULL", phoneNumber).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("user not found")
	}
	return &user, err
}

func (r *userRepository) GetByCognitoID(ctx context.Context, cognitoID string) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).Where("cognito_id = ? AND deleted_at IS NULL", cognitoID).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("user not found")
	}
	return &user, err
}

func (r *userRepository) Update(ctx context.Context, user *models.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

func (r *userRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.User{ID: id}).Error
}

func (r *userRepository) List(ctx context.Context, businessID string, page, limit int) ([]*models.User, int64, error) {
	var users []models.User
	var total int64

	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&models.User{}).Where("business_id = ? AND deleted_at IS NULL", businessID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.User, len(users))
	for i := range users {
		result[i] = &users[i]
	}

	return result, total, nil
}
