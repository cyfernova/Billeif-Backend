package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
)

type permissionTestBusinessRepository struct {
	ownerID string
}

func (*permissionTestBusinessRepository) Create(context.Context, *models.BusinessProfile) error {
	return nil
}
func (r *permissionTestBusinessRepository) GetByID(_ context.Context, id string) (*models.BusinessProfile, error) {
	if id != "business-1" {
		return nil, errors.New("not found")
	}
	return &models.BusinessProfile{ID: id, OwnerID: r.ownerID}, nil
}
func (*permissionTestBusinessRepository) Update(context.Context, *models.BusinessProfile) error {
	return nil
}
func (*permissionTestBusinessRepository) Delete(context.Context, string) error { return nil }
func (*permissionTestBusinessRepository) List(context.Context, string, int, int) ([]*models.BusinessProfile, int64, error) {
	return nil, 0, nil
}

type permissionTestTeamRepository struct {
	role string
}

func (*permissionTestTeamRepository) Create(context.Context, *models.TeamMember) error { return nil }
func (*permissionTestTeamRepository) GetByID(context.Context, string, string) (*models.TeamMember, error) {
	return nil, errors.New("not found")
}
func (*permissionTestTeamRepository) GetByBusinessID(context.Context, string, int, int) ([]*models.TeamMember, int64, error) {
	return nil, 0, nil
}
func (*permissionTestTeamRepository) Update(context.Context, *models.TeamMember) error { return nil }
func (*permissionTestTeamRepository) Delete(context.Context, string) error             { return nil }
func (r *permissionTestTeamRepository) GetByUserID(_ context.Context, userID string) ([]*models.TeamMember, error) {
	return []*models.TeamMember{{BusinessID: "business-1", UserID: userID, Role: r.role, Status: "active"}}, nil
}

func TestRequireAllBranchesRejectsCurrentRestrictedScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(validatedBranchScopeAllKey, false)
		c.Set(validatedBranchScopeIDsKey, []string{"11111111-1111-1111-1111-111111111111"})
		c.Next()
	})
	router.GET("/api/v1/invoices", RequireAllBranches(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/invoices", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("restricted current branch scope status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestRequireAllBranchesAllowsCurrentBusinessWideScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(validatedBranchScopeAllKey, true)
		c.Set(validatedBranchScopeIDsKey, []string(nil))
		c.Next()
	})
	router.GET("/api/v1/invoices", RequireAllBranches(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/invoices", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("business-wide current branch scope status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestPartyAndRenderProfilePermissionsReturnRoleAppropriateHTTPStatus(t *testing.T) {
	permissions := []string{
		services.PermissionCustomersCreate,
		services.PermissionCustomersUpdate,
		services.PermissionCustomersDelete,
		services.PermissionVendorsCreate,
		services.PermissionVendorsUpdate,
		services.PermissionVendorsDelete,
		services.PermissionRenderProfilesCreate,
		services.PermissionRenderProfilesUpdate,
		services.PermissionRenderProfilesDelete,
	}
	tests := []struct {
		name       string
		userID     string
		ownerID    string
		role       string
		wantStatus int
	}{
		{name: "owner wildcard", userID: "owner-1", ownerID: "owner-1", role: "viewer", wantStatus: http.StatusNoContent},
		{name: "legacy admin wildcard", userID: "admin-1", ownerID: "owner-1", role: "admin", wantStatus: http.StatusNoContent},
		{name: "legacy accountant", userID: "accountant-1", ownerID: "owner-1", role: "accountant", wantStatus: http.StatusNoContent},
		{name: "viewer", userID: "viewer-1", ownerID: "owner-1", role: "viewer", wantStatus: http.StatusForbidden},
	}

	for _, test := range tests {
		for _, permission := range permissions {
			t.Run(test.name+"/"+permission, func(t *testing.T) {
				auth := services.NewBusinessAuthService(
					nil,
					&permissionTestBusinessRepository{ownerID: test.ownerID},
					&permissionTestTeamRepository{role: test.role},
					logger.New(),
				)
				if !auth.UserHasBusinessAccess(context.Background(), test.userID, "business-1") {
					t.Fatal("active member must retain read-only business access")
				}

				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set("user_id", test.userID)
					c.Set("business_id", "business-1")
					c.Next()
				})
				router.POST("/mutation", RequirePermission(auth, permission), func(c *gin.Context) {
					c.Status(http.StatusNoContent)
				})

				request := httptest.NewRequest(http.MethodPost, "/mutation", nil)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != test.wantStatus {
					t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
				}
			})
		}
	}
}
