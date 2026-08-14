package spaces

import (
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
)

func TestSelectSpaceRecipient(t *testing.T) {
	candidates := []spaceRecipient{
		{
			ID: "user-1", DisplayName: "Alice Example",
			Username: "alice", Mail: "alice@example.test",
		},
		{
			ID: "user-2", DisplayName: "Bob Example",
			Username: "bob", Mail: "bob@example.test",
		},
	}
	for _, identifier := range []string{
		"user-1", "Alice Example", "ALICE", "alice@example.test",
	} {
		selected, err := selectSpaceRecipient(candidates, identifier, "user")
		if err != nil || selected.ID != "user-1" {
			t.Fatalf("%q: selected=%#v err=%v", identifier, selected, err)
		}
	}
}

func TestSelectSpaceRecipientRejectsAmbiguousAndMissingMatches(t *testing.T) {
	ambiguous := []spaceRecipient{
		{ID: "user-1", DisplayName: "Alex"},
		{ID: "user-2", DisplayName: "Alex"},
	}
	_, err := selectSpaceRecipient(ambiguous, "Alex", "user")
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "ambiguous") ||
		!strings.Contains(err.Error(), "user-1") {
		t.Fatalf("ambiguous: %v", err)
	}
	_, err = selectSpaceRecipient(nil, "missing", "user")
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "no user matched") {
		t.Fatalf("missing: %v", err)
	}
}
