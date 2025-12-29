package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
)

type teamMemberRepository struct {
	db *gorm.DB
}

func NewTeamMemberRepository(db *gorm.DB) TeamMemberRepository {
	return &teamMemberRepository{db: db}
}

func (r *teamMemberRepository) Create(ctx context.Context, member *models.TeamMember) error {
	return r.db.WithContext(ctx).Create(member).Error
}

func (r *teamMemberRepository) GetByID(ctx context.Context, id string) (*models.TeamMember, error) {
	var member models.TeamMember
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("team member not found")
	}
	return &member, err
}

func (r *teamMemberRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.TeamMember, int64, error) {
	var members []models.TeamMember
	var total int64

	offset := (page - 1) * limit

	query := r.db.WithContext(ctx).Model(&models.TeamMember{}).Where("business_id = ?", businessID)

	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&members).Error; err != nil {
		return nil, err
	}

	return members, total, nil
}

func (r *teamMemberRepository) Update(ctx context.Context, member *models.TeamMember) error {
	return r.db.WithContext(ctx).Save(member).Error
}

func (r *teamMemberRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&models.TeamMember{ID: id}).Error
}

func (r *teamMemberRepository) GetByUserID(ctx context.Context, userID string) ([]*models.TeamMember, error) {
	var members []models.TeamMember
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&members).Error
	if err != nil {
		return nil, err
	}
	return members, nil
}
