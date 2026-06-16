package api

import (
	"fmt"
	"regexp"
	"strings"
)

const maxLogValueLen = 512

var (
	idRegex       = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	ctrlCharRegex = regexp.MustCompile(`[\x00-\x1f\x7f]`)

	validComparisonModes = map[string]struct{}{
		"sequential":      {},
		"baseline":        {},
		"rolling_average": {},
	}
	validSeverities = map[string]struct{}{
		"critical": {},
		"major":    {},
		"minor":    {},
		"low":      {},
		"medium":   {},
		"high":     {},
	}
)

// ValidateID enforces a tight character class on opaque identifiers received
// from path or query parameters (run IDs, baseline names, test names, etc.).
// Returning an error here lets the handler reject log-injection payloads (CR,
// LF, control bytes) before any logging happens.
func ValidateID(name, value string) error {
	if !idRegex.MatchString(value) {
		return fmt.Errorf("invalid %s: must match [A-Za-z0-9._-]{1,128}", name)
	}
	return nil
}

// ValidateComparisonMode rejects any value outside the known regression
// comparison-mode enum. An empty string is allowed so callers can default it.
func ValidateComparisonMode(mode string) error {
	if mode == "" {
		return nil
	}
	if _, ok := validComparisonModes[mode]; !ok {
		return fmt.Errorf("invalid comparison_mode: %s", mode)
	}
	return nil
}

// ValidateSeverity rejects any value outside the known severity enum. An
// empty string is allowed so callers can omit the filter.
func ValidateSeverity(s string) error {
	if s == "" {
		return nil
	}
	if _, ok := validSeverities[s]; !ok {
		return fmt.Errorf("invalid severity: %s", s)
	}
	return nil
}

// SanitizeLogValue scrubs ASCII control characters (CR, LF, NUL, etc.) and
// truncates to maxLogValueLen. Use it for fields that legitimately accept
// free-form user input but still get logged (request paths, user agents,
// Grafana query targets, acknowledger names, etc.).
func SanitizeLogValue(s string) string {
	s = ctrlCharRegex.ReplaceAllString(s, "")
	if len(s) > maxLogValueLen {
		s = s[:maxLogValueLen] + "..."
	}
	return strings.TrimSpace(s)
}
