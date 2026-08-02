package app

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func profileIdentity(selected profile) string {
	switch selected.AuthType {
	case "oidc":
		if selected.Issuer != "" && selected.Subject != "" {
			return identityKey("oidc", selected.Issuer, selected.Subject)
		}
	case "basic":
		if selected.Username != "" {
			return identityKey(
				"basic", selected.Server, selected.Username,
			)
		}
	}
	return ""
}

func identityKey(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "v1:" + hex.EncodeToString(sum[:])
}

func clearDefaultSpaceAfterIdentityChange(selected *profile) {
	if selected.DefaultSpace == "" {
		selected.DefaultSpaceOwner = ""
		return
	}
	currentIdentity := profileIdentity(*selected)
	owner := selected.DefaultSpaceOwner
	if currentIdentity == "" || owner == "" || owner != currentIdentity {
		selected.DefaultSpace = ""
		selected.DefaultSpaceOwner = ""
		return
	}
	selected.DefaultSpaceOwner = currentIdentity
}
