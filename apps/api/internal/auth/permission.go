package auth

// Permissions map roughly to spec §19's example table. There is no
// generic "read" vs "write" split — each permission names a concrete
// capability so RolePermissions reads as a direct transcription of the
// spec's example, not an abstraction over it.
const (
	PermReadAll                  = "read_all"
	PermRunApprovedTests         = "run_approved_tests"
	PermViewArtifacts            = "view_artifacts"
	PermGenerateTests            = "generate_tests"
	PermGenerateFixProposals     = "generate_fix_proposals"
	PermApplyWorkspace           = "apply_workspace"
	PermApproveTestPlans         = "approve_test_plans"
	PermApproveRepositoryPatches = "approve_repository_patches"
	PermConfigureProviders       = "configure_providers"
	PermConfigureEnvironments    = "configure_environments"
	PermManageUsers              = "manage_users"
)

// RolePermissions is the fixed role -> permission set (spec §19).
// Administrator is a superset of every other role rather than a
// separately-curated list, since spec's own example describes it as
// the role that configures the system on top of everything else.
var RolePermissions = map[string]map[string]bool{
	RoleViewer: set(PermReadAll),
	RoleTester: set(PermReadAll, PermRunApprovedTests, PermViewArtifacts),
	RoleDeveloper: set(
		PermReadAll, PermViewArtifacts, PermGenerateTests, PermGenerateFixProposals, PermApplyWorkspace,
	),
	RoleApprover: set(
		PermReadAll, PermViewArtifacts, PermApproveTestPlans, PermApproveRepositoryPatches,
	),
	RoleAdministrator: set(
		PermReadAll, PermRunApprovedTests, PermViewArtifacts, PermGenerateTests, PermGenerateFixProposals,
		PermApplyWorkspace, PermApproveTestPlans, PermApproveRepositoryPatches,
		PermConfigureProviders, PermConfigureEnvironments, PermManageUsers,
	),
}

func set(perms ...string) map[string]bool {
	m := make(map[string]bool, len(perms))
	for _, p := range perms {
		m[p] = true
	}
	return m
}

// HasPermission reports whether role grants permission. An unrecognized
// role has no permissions at all — never treated as an unrestricted
// default.
func HasPermission(role, permission string) bool {
	return RolePermissions[role][permission]
}
