package services

import "testing"

func TestLegacyRolePermissionsIncludePaymentBoundaries(t *testing.T) {
	accountantPermissions := legacyRolePermissions("accountant")
	if !hasPermission(accountantPermissions, PermissionPaymentsManage) || !hasPermission(accountantPermissions, PermissionPaymentsView) {
		t.Fatalf("expected accountant to receive payment manage/view permissions, got %#v", accountantPermissions)
	}
	if !hasPermission(accountantPermissions, PermissionAccountingManage) || !hasPermission(accountantPermissions, PermissionBankingManage) {
		t.Fatalf("expected accountant to receive accounting and banking permissions, got %#v", accountantPermissions)
	}

	viewerPermissions := legacyRolePermissions("viewer")
	if !hasPermission(viewerPermissions, PermissionPaymentsView) {
		t.Fatalf("expected viewer to receive payment view permission, got %#v", viewerPermissions)
	}
	if hasPermission(viewerPermissions, PermissionPaymentsManage) {
		t.Fatalf("viewer must not receive payment manage permission: %#v", viewerPermissions)
	}
	if hasPermission(viewerPermissions, PermissionAccountingManage) || hasPermission(viewerPermissions, PermissionBankingManage) {
		t.Fatalf("viewer must not receive accounting or banking mutation permissions: %#v", viewerPermissions)
	}
}

func TestLegacyRolesSeparatePartyAndRenderProfileMutations(t *testing.T) {
	mutationPermissions := []string{
		"projects.manage",
		"customers.create",
		"customers.update",
		"customers.delete",
		"vendors.create",
		"vendors.update",
		"vendors.delete",
		"render_profiles.create",
		"render_profiles.update",
		"render_profiles.delete",
	}

	for _, permission := range mutationPermissions {
		if !hasPermission(legacyRolePermissions("admin"), permission) {
			t.Errorf("legacy admin must receive %q through its wildcard", permission)
		}
		if !hasPermission(legacyRolePermissions("accountant"), permission) {
			t.Errorf("legacy accountant must receive %q", permission)
		}
		if hasPermission(legacyRolePermissions("viewer"), permission) {
			t.Errorf("legacy viewer must not receive %q", permission)
		}
	}
}

func TestVoicePermissionIsLimitedToOwnerAndLegacyAdmin(t *testing.T) {
	if !hasPermission(legacyRolePermissions("admin"), PermissionVoiceUse) {
		t.Fatal("legacy admin must receive voice:use")
	}
	for _, role := range []string{"accountant", "viewer", ""} {
		if hasPermission(legacyRolePermissions(role), PermissionVoiceUse) {
			t.Fatalf("legacy %q must not receive voice:use", role)
		}
	}
}

func TestBranchScopeAllowsOnlyListedBranches(t *testing.T) {
	if !branchScopeAllows(false, []string{"branch-1", "branch-2"}, "branch-2") {
		t.Fatal("expected listed branch to be allowed")
	}
	if branchScopeAllows(false, []string{"branch-1"}, "branch-2") {
		t.Fatal("expected unlisted branch to be denied")
	}
	if !branchScopeAllows(true, nil, "branch-2") {
		t.Fatal("expected all-branch scope to allow branch")
	}
}

func hasPermission(permissions []string, target string) bool {
	for _, permission := range permissions {
		if permission == target || permission == "*" {
			return true
		}
	}
	return false
}
