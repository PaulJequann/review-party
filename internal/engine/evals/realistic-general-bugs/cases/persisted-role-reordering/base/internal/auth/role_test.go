package auth

import "testing"

func TestRoleLabels(t *testing.T) {
	if RoleViewer.String() != "viewer" {
		t.Fatal("viewer label changed")
	}
	if RoleAdministrator.String() != "administrator" {
		t.Fatal("administrator label changed")
	}
}
