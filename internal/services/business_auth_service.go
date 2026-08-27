package services

import (
	"context"
	"encoding/json"
	"strings"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"gorm.io/gorm"
)

type BusinessAuthService struct {
	db           *gorm.DB
	businessRepo interfaces.BusinessRepository
	teamRepo     interfaces.TeamMemberRepository
	log          *logger.Logger
}

func NewBusinessAuthService(db *gorm.DB, businessRepo interfaces.BusinessRepository, teamRepo interfaces.TeamMemberRepository, log *logger.Logger) *BusinessAuthService {
	return &BusinessAuthService{
		db:           db,
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

	member, err := s.lookupActiveMembership(ctx, userID, businessID)
	if err != nil {
		s.log.Debug("failed to get team membership", "user_id", userID, "business_id", businessID, "error", err)
		return false
	}
	return member != nil
}

func (s *BusinessAuthService) UserHasPermission(ctx context.Context, userID, businessID, permission string) bool {
	if userID == "" || businessID == "" || strings.TrimSpace(permission) == "" {
		return false
	}
	business, err := s.businessRepo.GetByID(ctx, businessID)
	if err == nil && business.OwnerID == userID {
		return true
	}

	member, err := s.lookupActiveMembership(ctx, userID, businessID)
	if err != nil || member == nil {
		return false
	}

	if override, ok := permissionOverrideValue(member.PermissionOverrides, permission); ok {
		return override
	}

	if member.RoleID != nil && *member.RoleID != "" && s.db != nil {
		var count int64
		if err := s.db.WithContext(ctx).
			Model(&models.RolePermission{}).
			Joins("JOIN roles ON roles.id = role_permissions.role_id AND roles.deleted_at IS NULL").
			Where("role_permissions.role_id = ? AND role_permissions.permission_key = ? AND role_permissions.deleted_at IS NULL", *member.RoleID, permission).
			Count(&count).Error; err == nil && count > 0 {
			return true
		}
	}

	for _, candidate := range legacyRolePermissions(member.Role) {
		if candidate == permission || candidate == "*" {
			return true
		}
	}
	return false
}

func (s *BusinessAuthService) UserHasBranchAccess(ctx context.Context, userID, businessID, branchID string) bool {
	if branchID == "" {
		return true
	}
	allBranches, branchIDs, ok := s.UserBranchScope(ctx, userID, businessID)
	if !ok {
		return false
	}
	return branchScopeAllows(allBranches, branchIDs, branchID)
}

func (s *BusinessAuthService) UserBranchScope(ctx context.Context, userID, businessID string) (bool, []string, bool) {
	if userID == "" || businessID == "" {
		return false, nil, false
	}
	business, err := s.businessRepo.GetByID(ctx, businessID)
	if err == nil && business.OwnerID == userID {
		return true, nil, true
	}

	member, err := s.lookupActiveMembership(ctx, userID, businessID)
	if err != nil || member == nil {
		return false, nil, false
	}
	scopes := branchScopes(member.BranchScopeJSON)
	if len(scopes) == 0 {
		return true, nil, true
	}
	for _, scope := range scopes {
		if scope == "*" {
			return true, nil, true
		}
	}
	return false, scopes, true
}

func branchScopeAllows(allBranches bool, branchIDs []string, branchID string) bool {
	if branchID == "" || allBranches {
		return true
	}
	for _, scope := range branchIDs {
		if scope == branchID {
			return true
		}
	}
	return false
}

func (s *BusinessAuthService) lookupActiveMembership(ctx context.Context, userID, businessID string) (*models.TeamMember, error) {
	if s.db != nil {
		var member models.TeamMember
		err := s.db.WithContext(ctx).
			Where("user_id = ? AND business_id = ? AND deleted_at IS NULL", userID, businessID).
			Order("created_at ASC").
			First(&member).Error
		if err == nil {
			if member.Status == "" || strings.EqualFold(member.Status, "active") {
				return &member, nil
			}
			return nil, nil
		}
		if err != nil && err != gorm.ErrRecordNotFound {
			return nil, err
		}
	}

	members, err := s.teamRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, member := range members {
		if member.BusinessID == businessID && (member.Status == "" || strings.EqualFold(member.Status, "active")) {
			return member, nil
		}
	}
	return nil, nil
}

func permissionOverrideValue(raw string, permission string) (bool, bool) {
	if strings.TrimSpace(raw) == "" {
		return false, false
	}
	var overrides map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &overrides); err != nil {
		return false, false
	}
	if value, ok := overrides[permission]; ok {
		boolValue, ok := value.(bool)
		return boolValue, ok
	}
	if value, ok := overrides["*"]; ok {
		boolValue, ok := value.(bool)
		return boolValue, ok
	}
	return false, false
}

func branchScopes(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var scopes []string
	if err := json.Unmarshal([]byte(raw), &scopes); err != nil {
		return nil
	}
	filtered := scopes[:0]
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope != "" {
			filtered = append(filtered, scope)
		}
	}
	return filtered
}

func legacyRolePermissions(role string) []string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin":
		return []string{"*", PermissionVoiceUse}
	case "accountant":
		return []string{
			PermissionDocumentsManage,
			PermissionDocumentsExport,
			PermissionCustomersCreate,
			PermissionCustomersUpdate,
			PermissionCustomersDelete,
			PermissionVendorsCreate,
			PermissionVendorsUpdate,
			PermissionVendorsDelete,
			PermissionRenderProfilesCreate,
			PermissionRenderProfilesUpdate,
			PermissionRenderProfilesDelete,
			PermissionProductsManage,
			PermissionProductsView,
			PermissionPaymentsManage,
			PermissionPaymentsView,
			PermissionStorefrontManage,
			PermissionStorefrontView,
			PermissionOrdersManage,
			PermissionOrdersView,
			PermissionTeamsView,
			PermissionBranchesView,
			PermissionDriveManage,
			PermissionDriveView,
			PermissionNotificationsManage,
			PermissionSubscriptionsView,
			PermissionReportsExport,
			PermissionReportsShare,
			PermissionReportsView,
			PermissionPOSOperate,
			PermissionAgentsView,
		}
	case "viewer":
		return []string{
			PermissionProductsView,
			PermissionPaymentsView,
			PermissionStorefrontView,
			PermissionOrdersView,
			PermissionTeamsView,
			PermissionBranchesView,
			PermissionDriveView,
			PermissionSubscriptionsView,
			PermissionAgentsView,
		}
	default:
		return nil
	}
}
