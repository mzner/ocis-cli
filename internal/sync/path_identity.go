package sync

import (
	"fmt"
	"sort"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// ValidatePathIdentity rejects trees that cannot be represented safely on a
// case-insensitive or normalization-insensitive filesystem. Exact spellings
// remain untouched; the folded form is used only to detect ambiguity.
func ValidatePathIdentity(snapshots ...Snapshot) error {
	spellings := make(map[string]map[string]struct{})
	for _, snapshot := range snapshots {
		for relative := range snapshot {
			identity := cases.Fold().String(norm.NFC.String(relative))
			if spellings[identity] == nil {
				spellings[identity] = make(map[string]struct{})
			}
			spellings[identity][relative] = struct{}{}
		}
	}
	identities := make([]string, 0, len(spellings))
	for identity := range spellings {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	for _, identity := range identities {
		if len(spellings[identity]) < 2 {
			continue
		}
		paths := make([]string, 0, len(spellings[identity]))
		for relative := range spellings[identity] {
			paths = append(paths, relative)
		}
		sort.Strings(paths)
		return fmt.Errorf(
			"paths %q and %q differ only by case or Unicode normalization",
			paths[0], paths[1],
		)
	}
	return nil
}
