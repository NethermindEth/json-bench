package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The error rate feeds every regression verdict, so comparing a run that
// counted JSON-RPC errors against one that could not see them reports a
// regression when nothing about the target changed.
func TestCompareSemantics(t *testing.T) {
	cases := map[string]struct {
		baseline   string
		current    string
		comparable bool
		verified   bool
		reason     string
	}{
		"both rpc-aware": {
			baseline: ErrorRateSemanticsRPCAware, current: ErrorRateSemanticsRPCAware,
			comparable: true, verified: true,
		},
		"both http-only": {
			baseline: ErrorRateSemanticsHTTPOnly, current: ErrorRateSemanticsHTTPOnly,
			comparable: true, verified: true,
		},
		"across the change": {
			baseline: ErrorRateSemanticsHTTPOnly, current: ErrorRateSemanticsRPCAware,
			comparable: false, verified: true, reason: "counted errors differently",
		},
		"across the change, the other way": {
			baseline: ErrorRateSemanticsRPCAware, current: ErrorRateSemanticsHTTPOnly,
			comparable: false, verified: true, reason: "counted errors differently",
		},
		// An unrecorded value is not proof of a mismatch, so the comparison
		// proceeds but says it could not be checked.
		"baseline unrecorded": {
			baseline: "", current: ErrorRateSemanticsRPCAware,
			comparable: true, verified: false, reason: "could not be verified",
		},
		"current unrecorded": {
			baseline: ErrorRateSemanticsRPCAware, current: "",
			comparable: true, verified: false, reason: "could not be verified",
		},
		"neither recorded": {
			baseline: "", current: "",
			comparable: true, verified: false, reason: "could not be verified",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			verdict := CompareSemantics(tc.baseline, tc.current)
			assert.Equal(t, tc.comparable, verdict.Comparable)
			assert.Equal(t, tc.verified, verdict.Verified)
			if tc.reason != "" {
				assert.Contains(t, verdict.Reason, tc.reason)
			}
			assert.Equal(t, tc.baseline, verdict.BaselineSemantics)
			assert.Equal(t, tc.current, verdict.CurrentSemantics)
		})
	}
}

// A refusal has to name both sides, or an operator cannot tell which run to
// re-measure.
func TestIncomparableVerdictNamesBothSides(t *testing.T) {
	verdict := CompareSemantics(ErrorRateSemanticsHTTPOnly, ErrorRateSemanticsRPCAware)

	assert.False(t, verdict.Comparable)
	assert.Contains(t, verdict.Reason, ErrorRateSemanticsHTTPOnly)
	assert.Contains(t, verdict.Reason, ErrorRateSemanticsRPCAware)
}
