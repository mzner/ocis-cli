package harness

import (
	"strings"
	"testing"
)

func TestSanitizerRemovesSensitiveValues(t *testing.T) {
	input := strings.Join([]string{
		"Authorization: Bearer access-value",
		"authorization='Basic basic-value'",
		`{"access_token":"access","refresh_token":"refresh",` +
			`"client_secret":"client","password":"password"}`,
		"https://example.test/callback?client_secret=query&password=form",
		"https://example.test/callback?code=auth-code&state=oauth-state",
		"Set-Cookie: session=session-value; Secure",
		"Cookie: session=session-value",
	}, "\n")
	result := NewSanitizer().Sanitize(input)
	for _, secret := range []string{
		"access-value", "basic-value", `:"access"`, `:"refresh"`,
		`:"client"`, `:"password"`, "=query", "=form", "auth-code",
		"oauth-state", "session-value",
	} {
		if strings.Contains(result, secret) {
			t.Fatalf("sanitized output contains %q:\n%s", secret, result)
		}
	}
	if count := strings.Count(result, "[REDACTED]"); count < 12 {
		t.Fatalf("redactions = %d, want at least 12:\n%s", count, result)
	}
}
