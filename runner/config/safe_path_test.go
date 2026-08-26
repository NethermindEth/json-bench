package config

import (
	"path/filepath"
	"testing"
)

func TestSafeReadPathUnder(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"descendant", filepath.Join(root, "corpus", "calls.jsonl"), false},
		{"root itself", root, false},
		{"direct child", filepath.Join(root, "calls.jsonl"), false},
		{"traversal out", filepath.Join(root, "..", "elsewhere", "id_rsa"), true},
		{"unrelated absolute", "/etc/passwd", true},
		{"empty", "", true},
	}
	for _, tc := range tests {
		got, err := SafeReadPathUnder(root, tc.path)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: expected an error, got %q", tc.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
			continue
		}
		if !filepath.IsAbs(got) {
			t.Errorf("%s: expected an absolute path, got %q", tc.name, got)
		}
	}
}

func TestSafeReadPathUnderRelativeRoot(t *testing.T) {
	if _, err := SafeReadPathUnder("corpus", "corpus/calls.jsonl"); err != nil {
		t.Errorf("relative root with relative descendant: %v", err)
	}
	if _, err := SafeReadPathUnder("corpus", "other/calls.jsonl"); err == nil {
		t.Error("expected a relative sibling directory to be rejected")
	}
	if _, err := SafeReadPathUnder("", "corpus/calls.jsonl"); err == nil {
		t.Error("expected an empty root to be rejected")
	}
}
