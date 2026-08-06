package services

import "testing"

func TestLegacyRolePermissionsIncludePaymentBoundaries(t *testing.T) {
	accountantPermissions := legacyRolePermissions("accountant")
	if !hasPermission(accountantPermissions, PermissionPaymentsManage) || !hasPermission(accountantPermissions, PermissionPaymentsView) {
		t.Fatalf("expected accountant to receive payment manage/view permissions, got %#v", accountantPermissions)
	}

	viewerPermissions := legacyRolePermissions("viewer")
	if !hasPermission(viewerPermissions, PermissionPaymentsView) {
		t.Fatalf("expected viewer to receive payment view permission, got %#v", viewerPermissions)
	}
	if hasPermission(viewerPermissions, PermissionPaymentsManage) {
		t.Fatalf("viewer must not receive payment manage permission: %#v", viewerPermissions)
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
		if permission == target {
			return true
		}
	}
	return false
}
