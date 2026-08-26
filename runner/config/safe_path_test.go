package config

import (
	"os"
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

// A symlink inside the root pointing outside it must be rejected: it is the
// target that gets read, so a purely lexical containment check would let a
// corpus tree pull in an arbitrary file.
func TestSafeReadPathUnderRejectsEscapingSymlink(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "corpus")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	outside := filepath.Join(base, "secret.jsonl")
	if err := os.WriteFile(outside, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	escaping := filepath.Join(root, "link.jsonl")
	if err := os.Symlink(outside, escaping); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if got, err := SafeReadPathUnder(root, escaping); err == nil {
		t.Errorf("a symlink to %s should be rejected, got %q", outside, got)
	}

	// A symlink that stays inside the root is still fine.
	inside := filepath.Join(root, "real.jsonl")
	if err := os.WriteFile(inside, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write inside file: %v", err)
	}
	internal := filepath.Join(root, "alias.jsonl")
	if err := os.Symlink(inside, internal); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := SafeReadPathUnder(root, internal); err != nil {
		t.Errorf("a symlink within the root should be accepted: %v", err)
	}
}

// A path that does not exist yet cannot be resolved, so the check falls back to
// the lexical comparison instead of failing.
func TestSafeReadPathUnderUnresolvablePath(t *testing.T) {
	root := t.TempDir()
	if _, err := SafeReadPathUnder(root, filepath.Join(root, "not-created-yet.jsonl")); err != nil {
		t.Errorf("expected a non-existent descendant to pass: %v", err)
	}
	if _, err := SafeReadPathUnder(root, filepath.Join(root, "..", "elsewhere.jsonl")); err == nil {
		t.Error("expected a non-existent path outside the root to be rejected")
	}
}
