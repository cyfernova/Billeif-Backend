package services

import (
	"context"

	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type BusinessAuthService struct {
	businessRepo interfaces.BusinessRepository
	teamRepo     interfaces.TeamMemberRepository
	log          *logger.Logger
}

func NewBusinessAuthService(businessRepo interfaces.BusinessRepository, teamRepo interfaces.TeamMemberRepository, log *logger.Logger) *BusinessAuthService {
	return &BusinessAuthService{
		businessRepo: businessRepo,
		teamRepo:     teamRepo,
		log:          log,
	}
}

// UserHasBusinessAccess checks if a user has access to a business.
// A user has access if they are the owner or a team member of the business.
func (s *BusinessAuthService) UserHasBusinessAccess(ctx context.Context, userID, businessID string) bool {
	if userID == "" || businessID == "" {
		return false
	}

	// Check if user is the owner
	business, err := s.businessRepo.GetByID(ctx, businessID)
	if err != nil {
		s.log.Debug("business not found for access check", "business_id", businessID, "error", err)
		return false
	}

	if business.OwnerID == userID {
		return true
	}

	// Check if user is a team member
	members, err := s.teamRepo.GetByUserID(ctx, userID)
	if err != nil {
		s.log.Debug("failed to get team memberships", "user_id", userID, "error", err)
		return false
	}

	for _, member := range members {
		if member.BusinessID == businessID {
			return true
		}
	}

	return false
}
