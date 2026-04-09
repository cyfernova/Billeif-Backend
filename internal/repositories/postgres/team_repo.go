package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type teamMemberRepository struct {
	db *gorm.DB
}

func NewTeamMemberRepository(db *gorm.DB) interfaces.TeamMemberRepository {
	return &teamMemberRepository{db: db}
}

func (r *teamMemberRepository) Create(ctx context.Context, member *models.TeamMember) error {
	if err := r.db.WithContext(ctx).Create(member).Error; err != nil {
		return err
	}
	loaded, err := r.GetByID(ctx, member.ID, member.BusinessID)
	if err != nil {
		return err
	}
	*member = *loaded
	return nil
}

func (r *teamMemberRepository) GetByID(ctx context.Context, id, businessID string) (*models.TeamMember, error) {
	var member models.TeamMember
	err := r.baseQuery(ctx, businessID).
		Where("id = ?", id).
		First(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("team member not found")
	}
	return &member, err
}

func (r *teamMemberRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.TeamMember, int64, error) {
	var members []models.TeamMember
	var total int64

	offset := (page - 1) * limit

	query := r.baseQuery(ctx, businessID).Order("created_at DESC")

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := query.Offset(offset).Limit(limit).Find(&members).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.TeamMember, len(members))
	for i := range members {
		result[i] = &members[i]
	}

	return result, total, nil
}

func (r *teamMemberRepository) Update(ctx context.Context, member *models.TeamMember) error {
	if err := r.db.WithContext(ctx).Save(member).Error; err != nil {
		return err
	}
	loaded, err := r.GetByID(ctx, member.ID, member.BusinessID)
	if err != nil {
		return err
	}
	*member = *loaded
	return nil
}

func (r *teamMemberRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.TeamMember{}).Error
}

func (r *teamMemberRepository) GetByUserID(ctx context.Context, userID string) ([]*models.TeamMember, error) {
	var members []models.TeamMember
	err := r.db.WithContext(ctx).
		Preload("AssignedRole", "deleted_at IS NULL").
		Where("user_id = ? AND deleted_at IS NULL", userID).
		Order("created_at DESC").
		Limit(defaultUnpaginatedQueryLimit).
		Find(&members).Error
	if err != nil {
		return nil, err
	}

	result := make([]*models.TeamMember, len(members))
	for i := range members {
		result[i] = &members[i]
	}

	return result, nil
}

func (r *teamMemberRepository) baseQuery(ctx context.Context, businessID string) *gorm.DB {
	return r.db.WithContext(ctx).
		Model(&models.TeamMember{}).
		Preload("AssignedRole", "deleted_at IS NULL").
		Where("business_id = ? AND deleted_at IS NULL", businessID)
}
