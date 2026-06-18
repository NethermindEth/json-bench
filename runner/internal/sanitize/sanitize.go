// Package sanitize centralizes the small input-cleaning helpers that
// the api, analysis, and storage packages share. Keeping them here
// avoids an api -> analysis import cycle while letting non-api code
// scrub user-controlled values before they reach logrus.
package sanitize

import (
	"errors"
	"regexp"
	"strings"
)

const maxLogValueLen = 512

var ctrlCharRegex = regexp.MustCompile(`[\x00-\x1f\x7f]`)

// LogValue scrubs ASCII control characters (CR, LF, NUL, etc.) and
// truncates to a fixed maximum length. Use it for fields that flow
// from a request into a logger entry, even when an upstream validator
// has already constrained the character class — CodeQL's taint
// analysis does not cross package boundaries.
func LogValue(s string) string {
	s = ctrlCharRegex.ReplaceAllString(s, "")
	if len(s) > maxLogValueLen {
		s = s[:maxLogValueLen] + "..."
	}
	return strings.TrimSpace(s)
}

// LogError returns an error whose message has been scrubbed of control
// bytes. Use it whenever an upstream error message may quote a
// user-controlled value (run IDs, baseline names, etc.) and is about to
// reach logrus.WithError or another structured-log sink.
func LogError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(LogValue(err.Error()))
}
