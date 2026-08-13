package auth

import "testing"

func TestRoleLabels(t *testing.T) {
	labels := map[Role]string{
		RoleViewer:        "viewer",
		RoleAdministrator: "administrator",
		RoleAuditor:       "auditor",
	}
	for role, expected := range labels {
		if role.String() != expected {
			t.Fatalf("role %d label = %q", role, role.String())
		}
	}
}
