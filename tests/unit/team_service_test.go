package unit

import (
	"context"
	"errors"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockTeamMemberRepository mocks the TeamMemberRepository interface
type MockTeamMemberRepository struct {
	mock.Mock
}

func (m *MockTeamMemberRepository) Create(ctx context.Context, member *models.TeamMember) error {
	args := m.Called(ctx, member)
	return args.Error(0)
}

func (m *MockTeamMemberRepository) GetByID(ctx context.Context, id, businessID string) (*models.TeamMember, error) {
	args := m.Called(ctx, id, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.TeamMember), args.Error(1)
}

func (m *MockTeamMemberRepository) GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.TeamMember, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]*models.TeamMember), args.Get(1).(int64), args.Error(2)
}

func (m *MockTeamMemberRepository) Update(ctx context.Context, member *models.TeamMember) error {
	args := m.Called(ctx, member)
	return args.Error(0)
}

func (m *MockTeamMemberRepository) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockTeamMemberRepository) GetByUserID(ctx context.Context, userID string) ([]*models.TeamMember, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.TeamMember), args.Error(1)
}

// TestCreateTeamMember_Success tests successful team member creation
func TestTeamService_Create_Success(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	input := services.CreateTeamMemberInput{
		BusinessID: "business-123",
		UserID:     "user-456",
		Role:       "admin",
	}

	mockRepo.On("Create", ctx, mock.MatchedBy(func(m *models.TeamMember) bool {
		return m.BusinessID == input.BusinessID && m.UserID == input.UserID && m.Role == input.Role
	})).Return(nil)

	member, err := svc.Create(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, member)
	assert.Equal(t, input.BusinessID, member.BusinessID)
	assert.Equal(t, input.UserID, member.UserID)
	assert.Equal(t, input.Role, member.Role)
	mockRepo.AssertExpectations(t)
}

// TestCreateTeamMember_RepositoryError tests team member creation with repository error
func TestTeamService_Create_RepositoryError(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	input := services.CreateTeamMemberInput{
		BusinessID: "business-123",
		UserID:     "user-456",
		Role:       "viewer",
	}

	mockRepo.On("Create", ctx, mock.AnythingOfType("*models.TeamMember")).Return(errors.New("database error"))

	member, err := svc.Create(ctx, input)

	assert.Error(t, err)
	assert.Nil(t, member)
	assert.Contains(t, err.Error(), "failed to create team member")
	mockRepo.AssertExpectations(t)
}

// TestCreateTeamMember_AllRoles tests creating team members with each role
func TestTeamService_Create_AllRoles(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	roles := []string{"admin", "accountant", "viewer"}

	for _, role := range roles {
		input := services.CreateTeamMemberInput{
			BusinessID: "business-123",
			UserID:     "user-456",
			Role:       role,
		}

		mockRepo.On("Create", ctx, mock.MatchedBy(func(m *models.TeamMember) bool {
			return m.Role == role
		})).Return(nil).Once()

		member, err := svc.Create(ctx, input)

		assert.NoError(t, err)
		assert.NotNil(t, member)
		assert.Equal(t, role, member.Role)
	}

	mockRepo.AssertExpectations(t)
}

// TestGetByBusiness_Success tests successful team member retrieval
func TestTeamService_GetByBusiness_Success(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	memberID := "member-456"

	expectedMember := &models.TeamMember{
		ID:         memberID,
		BusinessID: businessID,
		UserID:     "user-789",
		Role:       "admin",
	}

	mockRepo.On("GetByID", ctx, memberID, businessID).Return(expectedMember, nil)

	member, err := svc.GetByBusiness(ctx, businessID, memberID)

	assert.NoError(t, err)
	assert.NotNil(t, member)
	assert.Equal(t, memberID, member.ID)
	assert.Equal(t, businessID, member.BusinessID)
	mockRepo.AssertExpectations(t)
}

// TestGetByBusiness_NotFound tests team member not found scenario
func TestTeamService_GetByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	memberID := "nonexistent"

	mockRepo.On("GetByID", ctx, memberID, businessID).Return(nil, errors.New("team member not found"))

	member, err := svc.GetByBusiness(ctx, businessID, memberID)

	assert.Error(t, err)
	assert.Nil(t, member)
	mockRepo.AssertExpectations(t)
}

// TestList_Success tests listing team members with pagination
func TestTeamService_List_Success(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	page := 1
	limit := 10

	expectedMembers := []*models.TeamMember{
		{ID: "member-1", BusinessID: businessID, UserID: "user-1", Role: "admin"},
		{ID: "member-2", BusinessID: businessID, UserID: "user-2", Role: "accountant"},
	}

	mockRepo.On("GetByBusinessID", ctx, businessID, page, limit).Return(expectedMembers, int64(2), nil)

	members, total, err := svc.List(ctx, businessID, page, limit)

	assert.NoError(t, err)
	assert.Len(t, members, 2)
	assert.Equal(t, int64(2), total)
	mockRepo.AssertExpectations(t)
}

// TestList_EmptyResult tests listing with no team members
func TestTeamService_List_EmptyResult(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 10).Return([]*models.TeamMember{}, int64(0), nil)

	members, total, err := svc.List(ctx, businessID, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, members, 0)
	assert.Equal(t, int64(0), total)
	mockRepo.AssertExpectations(t)
}

// TestList_RepositoryError tests listing with repository error
func TestTeamService_List_RepositoryError(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	mockRepo.On("GetByBusinessID", ctx, businessID, 1, 10).Return([]*models.TeamMember{}, int64(0), errors.New("database error"))

	members, _, err := svc.List(ctx, businessID, 1, 10)

	assert.Error(t, err)
	assert.Empty(t, members)
	mockRepo.AssertExpectations(t)
}

// TestList_Pagination tests listing with different page and limit values
func TestTeamService_List_Pagination(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"

	testCases := []struct {
		page    int
		limit   int
		members []*models.TeamMember
		total   int64
	}{
		{1, 10, []*models.TeamMember{{ID: "m1"}}, 1},
		{2, 10, []*models.TeamMember{}, 15},
		{1, 5, []*models.TeamMember{{ID: "m1"}, {ID: "m2"}}, 2},
	}

	for _, tc := range testCases {
		mockRepo.On("GetByBusinessID", ctx, businessID, tc.page, tc.limit).Return(tc.members, tc.total, nil).Once()

		members, total, err := svc.List(ctx, businessID, tc.page, tc.limit)

		assert.NoError(t, err)
		assert.Equal(t, tc.members, members)
		assert.Equal(t, tc.total, total)
	}

	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_Success tests successful team member update
func TestTeamService_UpdateByBusiness_Success(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	memberID := "member-456"

	existingMember := &models.TeamMember{
		ID:         memberID,
		BusinessID: businessID,
		UserID:     "user-789",
		Role:       "viewer",
	}

	mockRepo.On("GetByID", ctx, memberID, businessID).Return(existingMember, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(m *models.TeamMember) bool {
		return m.Role == "admin"
	})).Return(nil)

	input := services.UpdateTeamMemberInput{
		Role: "admin",
	}

	member, err := svc.UpdateByBusiness(ctx, businessID, memberID, input)

	assert.NoError(t, err)
	assert.NotNil(t, member)
	assert.Equal(t, "admin", member.Role)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_NotFound tests update on non-existent team member
func TestTeamService_UpdateByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	memberID := "nonexistent"

	mockRepo.On("GetByID", ctx, memberID, businessID).Return(nil, errors.New("team member not found"))

	input := services.UpdateTeamMemberInput{
		Role: "admin",
	}

	member, err := svc.UpdateByBusiness(ctx, businessID, memberID, input)

	assert.Error(t, err)
	assert.Nil(t, member)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_RepositoryError tests update with repository error
func TestTeamService_UpdateByBusiness_RepositoryError(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	memberID := "member-456"

	existingMember := &models.TeamMember{
		ID:         memberID,
		BusinessID: businessID,
		UserID:     "user-789",
		Role:       "viewer",
	}

	mockRepo.On("GetByID", ctx, memberID, businessID).Return(existingMember, nil)
	mockRepo.On("Update", ctx, mock.AnythingOfType("*models.TeamMember")).Return(errors.New("database error"))

	input := services.UpdateTeamMemberInput{
		Role: "admin",
	}

	member, err := svc.UpdateByBusiness(ctx, businessID, memberID, input)

	assert.Error(t, err)
	assert.Nil(t, member)
	mockRepo.AssertExpectations(t)
}

// TestUpdateByBusiness_ChangeRole tests changing role from admin to accountant
func TestTeamService_UpdateByBusiness_ChangeRole(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	memberID := "member-456"

	existingMember := &models.TeamMember{
		ID:         memberID,
		BusinessID: businessID,
		UserID:     "user-789",
		Role:       "admin",
	}

	mockRepo.On("GetByID", ctx, memberID, businessID).Return(existingMember, nil)
	mockRepo.On("Update", ctx, mock.MatchedBy(func(m *models.TeamMember) bool {
		return m.Role == "accountant"
	})).Return(nil)

	input := services.UpdateTeamMemberInput{
		Role: "accountant",
	}

	member, err := svc.UpdateByBusiness(ctx, businessID, memberID, input)

	assert.NoError(t, err)
	assert.NotNil(t, member)
	assert.Equal(t, "accountant", member.Role)
	mockRepo.AssertExpectations(t)
}

// TestDelete_Success tests successful team member deletion
func TestTeamService_Delete_Success(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	memberID := "member-456"

	mockRepo.On("Delete", ctx, memberID).Return(nil)

	err := svc.Delete(ctx, memberID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestDelete_RepositoryError tests deletion with repository error
func TestTeamService_Delete_RepositoryError(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	memberID := "member-456"

	mockRepo.On("Delete", ctx, memberID).Return(errors.New("database error"))

	err := svc.Delete(ctx, memberID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_Success tests successful scoped team member deletion
func TestTeamService_DeleteByBusiness_Success(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	memberID := "member-456"

	existingMember := &models.TeamMember{
		ID:         memberID,
		BusinessID: businessID,
		UserID:     "user-789",
		Role:       "admin",
	}

	mockRepo.On("GetByID", ctx, memberID, businessID).Return(existingMember, nil)
	mockRepo.On("Delete", ctx, memberID).Return(nil)

	err := svc.DeleteByBusiness(ctx, businessID, memberID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_NotFound tests scoped deletion when team member not found
func TestTeamService_DeleteByBusiness_NotFound(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	memberID := "nonexistent"

	mockRepo.On("GetByID", ctx, memberID, businessID).Return(nil, errors.New("team member not found"))

	err := svc.DeleteByBusiness(ctx, businessID, memberID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_WrongBusiness tests deletion when team member belongs to different business
func TestTeamService_DeleteByBusiness_WrongBusiness(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	memberID := "member-456"

	// Team member not found for this business (belongs to different business)
	mockRepo.On("GetByID", ctx, memberID, businessID).Return(nil, errors.New("team member not found"))

	err := svc.DeleteByBusiness(ctx, businessID, memberID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

// TestDeleteByBusiness_RepositoryError tests scoped deletion with repository error
func TestTeamService_DeleteByBusiness_RepositoryError(t *testing.T) {
	mockRepo := new(MockTeamMemberRepository)
	log := logger.New()

	svc := services.NewTeamService(mockRepo, log)

	ctx := context.Background()
	businessID := "business-123"
	memberID := "member-456"

	existingMember := &models.TeamMember{
		ID:         memberID,
		BusinessID: businessID,
		UserID:     "user-789",
		Role:       "admin",
	}

	mockRepo.On("GetByID", ctx, memberID, businessID).Return(existingMember, nil)
	mockRepo.On("Delete", ctx, memberID).Return(errors.New("database error"))

	err := svc.DeleteByBusiness(ctx, businessID, memberID)

	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}
