package spaces

import "testing"

func TestRoleSemanticAliases(t *testing.T) {
	for value, expected := range map[string]string{
		"Can view": "viewer", "viewer": "viewer", "read": "viewer",
		"Can edit with versions and trashbin": "editor", "write": "editor",
		"Can manage": "manager", "manager": "manager", "custom role": "",
	} {
		if actual := roleSemantic(value); actual != expected {
			t.Errorf("%q: got %q, want %q", value, actual, expected)
		}
	}
}
