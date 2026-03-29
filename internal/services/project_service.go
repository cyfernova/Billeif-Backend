package services

import (
	"context"
	"fmt"
	"strings"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"gorm.io/gorm"
)

type ProjectService struct {
	db  *gorm.DB
	log *logger.Logger
}

type CreateProjectInput struct {
	Name        string                 `json:"name" binding:"required"`
	Code        string                 `json:"code"`
	Description string                 `json:"description"`
	Color       string                 `json:"color"`
	IsActive    *bool                  `json:"is_active,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type UpdateProjectInput struct {
	Name        *string                `json:"name,omitempty"`
	Code        *string                `json:"code,omitempty"`
	Description *string                `json:"description,omitempty"`
	Color       *string                `json:"color,omitempty"`
	IsActive    *bool                  `json:"is_active,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

func NewProjectService(db *gorm.DB, log *logger.Logger) *ProjectService {
	return &ProjectService{db: db, log: log}
}

func (s *ProjectService) List(ctx context.Context, businessID string) ([]*models.Project, error) {
	var rows []models.Project
	if err := s.db.WithContext(ctx).
		Where("business_id = ? AND deleted_at IS NULL", businessID).
		Order("is_active DESC, name ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*models.Project, 0, len(rows))
	for i := range rows {
		result = append(result, &rows[i])
	}
	return result, nil
}

func (s *ProjectService) Create(ctx context.Context, businessID string, input CreateProjectInput) (*models.Project, error) {
	project := &models.Project{
		BusinessID:  businessID,
		Name:        strings.TrimSpace(input.Name),
		Code:        normalizeProjectCode(input.Code, input.Name),
		Description: strings.TrimSpace(input.Description),
		Color:       strings.TrimSpace(input.Color),
		IsActive:    boolValueOrDefault(input.IsActive, true),
		Metadata:    mustMarshalMap(input.Metadata),
	}
	if project.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if project.Code == "" {
		return nil, fmt.Errorf("code is required")
	}
	if err := s.db.WithContext(ctx).Create(project).Error; err != nil {
		return nil, err
	}
	return project, nil
}

func (s *ProjectService) Update(ctx context.Context, businessID, id string, input UpdateProjectInput) (*models.Project, error) {
	var project models.Project
	if err := s.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		First(&project).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("project not found")
		}
		return nil, err
	}

	if input.Name != nil {
		project.Name = strings.TrimSpace(*input.Name)
	}
	if input.Code != nil {
		project.Code = normalizeProjectCode(*input.Code, project.Name)
	}
	if input.Description != nil {
		project.Description = strings.TrimSpace(*input.Description)
	}
	if input.Color != nil {
		project.Color = strings.TrimSpace(*input.Color)
	}
	if input.IsActive != nil {
		project.IsActive = *input.IsActive
	}
	if input.Metadata != nil {
		project.Metadata = mustMarshalMap(input.Metadata)
	}
	if project.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if project.Code == "" {
		project.Code = normalizeProjectCode("", project.Name)
	}
	if err := s.db.WithContext(ctx).Save(&project).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

func (s *ProjectService) Delete(ctx context.Context, businessID, id string) error {
	result := s.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		Delete(&models.Project{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("project not found")
	}
	return nil
}

func normalizeProjectCode(code, name string) string {
	source := strings.TrimSpace(code)
	if source == "" {
		source = name
	}
	source = strings.ToUpper(source)
	var builder strings.Builder
	lastDash := false
	for _, r := range source {
		switch {
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		case !lastDash:
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}
