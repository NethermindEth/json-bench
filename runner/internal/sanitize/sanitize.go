// Package sanitize centralizes the small input-cleaning helpers that
// the api, analysis, and storage packages share. Keeping them here
// avoids an api -> analysis import cycle while letting non-api code
// scrub user-controlled values before they reach logrus.
package sanitize

import (
	"errors"
	"strings"
)

const maxLogValueLen = 512

// logSanitizer strips the bytes that enable log forgery (CR, LF, NUL)
// plus the rest of the C0 control range and DEL. Defined as a single
// strings.Replacer so CodeQL's go/log-injection query recognizes the
// .Replace call as a sanitizer barrier without needing a custom
// dataflow extension to cross our function boundary.
var logSanitizer = strings.NewReplacer(
	"\r", "", "\n", "", "\x00", "", "\x01", "", "\x02", "",
	"\x03", "", "\x04", "", "\x05", "", "\x06", "", "\x07", "",
	"\x08", "", "\x09", "", "\x0b", "", "\x0c", "", "\x0e", "",
	"\x0f", "", "\x10", "", "\x11", "", "\x12", "", "\x13", "",
	"\x14", "", "\x15", "", "\x16", "", "\x17", "", "\x18", "",
	"\x19", "", "\x1a", "", "\x1b", "", "\x1c", "", "\x1d", "",
	"\x1e", "", "\x1f", "", "\x7f", "",
)

// LogValue scrubs ASCII control characters (CR, LF, NUL, etc.) and
// truncates to a fixed maximum length. Use it for fields that flow
// from a request into a logger entry, even when an upstream validator
// has already constrained the character class — CodeQL's taint
// analysis does not cross package boundaries on its own.
func LogValue(s string) string {
	s = logSanitizer.Replace(s)
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
