package api

import (
	"strings"
	"testing"
)

func TestValidateID(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"uuid", "550e8400-e29b-41d4-a716-446655440000", false},
		{"slug", "my-run.v2_3", false},
		{"single char", "a", false},
		{"empty", "", true},
		{"contains slash", "abc/def", true},
		{"contains LF", "abc\ndef", true},
		{"contains CR", "abc\rdef", true},
		{"contains space", "abc def", true},
		{"too long", strings.Repeat("a", 129), true},
		{"max length", strings.Repeat("a", 128), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateID("runId", tc.value)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateID(%q) error = %v, wantErr = %v", tc.value, err, tc.wantErr)
			}
		})
	}
}

func TestValidateComparisonMode(t *testing.T) {
	tests := []struct {
		mode    string
		wantErr bool
	}{
		{"", false},
		{"sequential", false},
		{"baseline", false},
		{"rolling_average", false},
		{"bogus", true},
		{"SEQUENTIAL", true},
	}
	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			err := ValidateComparisonMode(tc.mode)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateComparisonMode(%q) error = %v, wantErr = %v", tc.mode, err, tc.wantErr)
			}
		})
	}
}

func TestValidateSeverity(t *testing.T) {
	tests := []struct {
		sev     string
		wantErr bool
	}{
		{"", false},
		{"critical", false},
		{"high", false},
		{"medium", false},
		{"low", false},
		{"major", false},
		{"minor", false},
		{"HIGH", true},
		{"unknown", true},
	}
	for _, tc := range tests {
		t.Run(tc.sev, func(t *testing.T) {
			err := ValidateSeverity(tc.sev)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateSeverity(%q) error = %v, wantErr = %v", tc.sev, err, tc.wantErr)
			}
		})
	}
}

