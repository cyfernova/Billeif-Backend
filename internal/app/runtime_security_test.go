package app

import (
	"os"
	"strings"
	"testing"
)

func TestPaymentRoutesRequirePaymentPermissions(t *testing.T) {
	source, err := os.ReadFile("runtime.go")
	if err != nil {
		t.Fatalf("read runtime routes: %v", err)
	}
	routes := string(source)

	requiredFragments := []string{
		`payments.GET("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsView), h.Payment.List)`,
		`payments.GET("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsView), h.Payment.Get)`,
		`payments.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsManage), wafUserWriteRL, h.Payment.Create)`,
		`payments.PUT("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsManage), wafUserWriteRL, h.Payment.Update)`,
		`payments.DELETE("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionPaymentsManage), wafUserWriteRL, h.Payment.Delete)`,
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(routes, fragment) {
			t.Fatalf("payment route is missing required permission gate: %s", fragment)
		}
	}
}

func TestTeamRoleMutationRoutesRequireRoleManagement(t *testing.T) {
	source, err := os.ReadFile("runtime.go")
	if err != nil {
		t.Fatalf("read runtime routes: %v", err)
	}
	routes := string(source)

	requiredFragments := []string{
		`teams.POST("", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTeamsManage), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionRolesManage), h.Team.Create)`,
		`teams.PUT("/:id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionTeamsManage), middleware.RequirePermission(svcs.BusinessAuth, services.PermissionRolesManage), h.Team.Update)`,
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(routes, fragment) {
			t.Fatalf("team mutation route is missing role-management gate: %s", fragment)
		}
	}
}

func TestBranchMutationRoutesExposeBranchScopeParam(t *testing.T) {
	source, err := os.ReadFile("runtime.go")
	if err != nil {
		t.Fatalf("read runtime routes: %v", err)
	}
	routes := string(source)

	requiredFragments := []string{
		`branches.PUT("/:branch_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionBranchesManage), h.Commerce.UpdateBranch)`,
		`branches.DELETE("/:branch_id", middleware.RequirePermission(svcs.BusinessAuth, services.PermissionBranchesManage), h.Commerce.DeleteBranch)`,
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(routes, fragment) {
			t.Fatalf("branch mutation route must expose branch_id to BusinessAuth: %s", fragment)
		}
	}
}
