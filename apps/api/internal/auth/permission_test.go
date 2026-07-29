package auth

import "testing"

func TestHasPermission_MatchesSpecExampleTable(t *testing.T) {
	cases := []struct {
		role       string
		permission string
		want       bool
	}{
		{RoleViewer, PermReadAll, true},
		{RoleViewer, PermRunApprovedTests, false},
		{RoleTester, PermRunApprovedTests, true},
		{RoleTester, PermApproveTestPlans, false},
		{RoleDeveloper, PermGenerateFixProposals, true},
		{RoleDeveloper, PermApproveRepositoryPatches, false},
		{RoleApprover, PermApproveRepositoryPatches, true},
		{RoleApprover, PermGenerateTests, false},
		{RoleAdministrator, PermConfigureProviders, true},
		{RoleAdministrator, PermApproveRepositoryPatches, true},
		{RoleAdministrator, PermRunApprovedTests, true},
	}
	for _, tc := range cases {
		if got := HasPermission(tc.role, tc.permission); got != tc.want {
			t.Errorf("HasPermission(%q, %q) = %v, want %v", tc.role, tc.permission, got, tc.want)
		}
	}
}

func TestHasPermission_UnrecognizedRoleHasNoPermissions(t *testing.T) {
	if HasPermission("not-a-real-role", PermReadAll) {
		t.Error("an unrecognized role must never be granted a permission")
	}
}

func TestValidRole(t *testing.T) {
	for _, r := range []string{RoleViewer, RoleTester, RoleDeveloper, RoleApprover, RoleAdministrator} {
		if !ValidRole(r) {
			t.Errorf("ValidRole(%q) = false, want true", r)
		}
	}
	if ValidRole("bogus") {
		t.Error("ValidRole(bogus) = true, want false")
	}
}
