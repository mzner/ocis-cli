package harness

import (
	"regexp"
	"strings"
)

type replacement struct {
	pattern *regexp.Regexp
	value   string
}

// Sanitizer removes credentials and session material from diagnostic logs.
type Sanitizer struct {
	replacements []replacement
}

// NewSanitizer constructs the deterministic integration-log sanitizer.
func NewSanitizer() Sanitizer {
	return Sanitizer{replacements: []replacement{
		{
			pattern: regexp.MustCompile(
				`(?i)(authorization["'=:\s]+)(?:bearer|basic)\s+[^\s"',]+`,
			),
			value: `${1}[REDACTED]`,
		},
		{
			pattern: regexp.MustCompile(
				`(?i)("(?:access_token|refresh_token|id_token|client_secret|` +
					`password|code|code_verifier|code_challenge|state)"\s*:\s*")[^"]*`,
			),
			value: `${1}[REDACTED]`,
		},
		{
			pattern: regexp.MustCompile(
				`(?i)((?:access_token|refresh_token|id_token|client_secret|` +
					`password|code|code_verifier|code_challenge|state|` +
					`session_state)=)[^&\s]+`,
			),
			value: `${1}[REDACTED]`,
		},
		{
			pattern: regexp.MustCompile(
				`(?i)((?:set-cookie|cookie)["'=:\s]+)[^\r\n]+`,
			),
			value: `${1}[REDACTED]`,
		},
	}}
}

// Sanitize removes sensitive values from text.
func (sanitizer Sanitizer) Sanitize(value string) string {
	for _, current := range sanitizer.replacements {
		value = current.pattern.ReplaceAllString(value, current.value)
	}
	return value
}

// SanitizeText sanitizes command diagnostics with the default policy.
func SanitizeText(value string) string {
	return strings.TrimSpace(NewSanitizer().Sanitize(value))
}
