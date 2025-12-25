package services

import (
	"context"

	"github.com/cyfernova/invoice-backend/internal/models"
	"github.com/cyfernova/invoice-backend/internal/repositories"
	"github.com/cyfernova/invoice-backend/internal/utils"
)

// UserService handles user management operations
type UserService struct {
	userRepo repositories.UserRepository
	cognito  *CognitoService
}

// NewUserService creates a new user service
func NewUserService(userRepo repositories.UserRepository, cognito *CognitoService) *UserService {
	return &UserService{
		userRepo: userRepo,
		cognito:  cognito,
	}
}

// UpdateProfileRequest represents profile update request
type UpdateProfileRequest struct {
	FirstName        *string
	LastName         *string
	Phone            *string
	ProfilePictureURL *string
}

// GetByID retrieves a user by ID
func (s *UserService) GetByID(ctx context.Context, id string) (*UserResponse, error) {
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, utils.ErrNotFound
	}

	if user == nil {
		return nil, utils.ErrNotFound
	}

	groups, _ := s.cognito.ListUserGroups(ctx, user.Email)

	return &UserResponse{
		ID:                user.ID.String(),
		Email:             user.Email,
		FirstName:         user.FirstName,
		LastName:          user.LastName,
		Phone:             user.Phone,
		ProfilePictureURL: user.ProfilePictureURL,
		Role:              user.Role,
		EmailVerified:     user.EmailVerified,
		Groups:            groups,
	}, nil
}

// GetByEmail retrieves a user by email
func (s *UserService) GetByEmail(ctx context.Context, email string) (*UserResponse, error) {
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return nil, utils.ErrNotFound
	}

	if user == nil {
		return nil, utils.ErrNotFound
	}

	groups, _ := s.cognito.ListUserGroups(ctx, user.Email)

	return &UserResponse{
		ID:                user.ID.String(),
		Email:             user.Email,
		FirstName:         user.FirstName,
		LastName:          user.LastName,
		Phone:             user.Phone,
		ProfilePictureURL: user.ProfilePictureURL,
		Role:              user.Role,
		EmailVerified:     user.EmailVerified,
		Groups:            groups,
	}, nil
}

// UpdateProfile updates user profile
func (s *UserService) UpdateProfile(ctx context.Context, userID string, req *UpdateProfileRequest) (*UserResponse, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, utils.ErrNotFound
	}

	if user == nil {
		return nil, utils.ErrNotFound
	}

	// Update fields
	if req.FirstName != nil {
		user.FirstName = *req.FirstName
	}
	if req.LastName != nil {
		user.LastName = *req.LastName
	}
	if req.Phone != nil {
		user.Phone = *req.Phone
	}
	if req.ProfilePictureURL != nil {
		user.ProfilePictureURL = *req.ProfilePictureURL
	}

	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, utils.ErrInternalServer
	}

	groups, _ := s.cognito.ListUserGroups(ctx, user.Email)

	return &UserResponse{
		ID:                user.ID.String(),
		Email:             user.Email,
		FirstName:         user.FirstName,
		LastName:          user.LastName,
		Phone:             user.Phone,
		ProfilePictureURL: user.ProfilePictureURL,
		Role:              user.Role,
		EmailVerified:     user.EmailVerified,
		Groups:            groups,
	}, nil
}

// List retrieves all users with pagination
func (s *UserService) List(ctx context.Context, page, perPage int) ([]*UserResponse, int64, error) {
	users, total, err := s.userRepo.List(ctx, page, perPage)
	if err != nil {
		return nil, 0, err
	}

	response := make([]*UserResponse, len(users))
	for i, user := range users {
		groups, _ := s.cognito.ListUserGroups(ctx, user.Email)
		response[i] = &UserResponse{
			ID:                user.ID.String(),
			Email:             user.Email,
			FirstName:         user.FirstName,
			LastName:          user.LastName,
			Phone:             user.Phone,
			ProfilePictureURL: user.ProfilePictureURL,
			Role:              user.Role,
			EmailVerified:     user.EmailVerified,
			Groups:            groups,
		}
	}

	return response, total, nil
}

// Delete deletes a user
func (s *UserService) Delete(ctx context.Context, userID string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return utils.ErrNotFound
	}

	if user == nil {
		return utils.ErrNotFound
	}

	// Delete from Cognito
	if err := s.cognito.AdminDeleteUser(ctx, user.Email); err != nil {
		// Log error but continue with database deletion
	}

	// Delete from database
	return s.userRepo.Delete(ctx, userID)
}

// SetRole updates user role
func (s *UserService) SetRole(ctx context.Context, userID string, role models.UserRole) error {
	if !models.IsValidRole(role) {
		return utils.ErrBadRequest
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return utils.ErrNotFound
	}

	if user == nil {
		return utils.ErrNotFound
	}

	user.Role = role
	return s.userRepo.Update(ctx, user)
}

// AddToGroup adds user to a Cognito group
func (s *UserService) AddToGroup(ctx context.Context, userID, groupName string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return utils.ErrNotFound
	}

	if user == nil {
		return utils.ErrNotFound
	}

	return s.cognito.AddUserToGroup(ctx, user.Email, groupName)
}
