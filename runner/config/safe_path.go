package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SafeReadPath validates a YAML-supplied file path before it is passed to
// os.ReadFile/os.Open. Absolute paths and paths that escape the current
// working directory after cleaning are rejected so a malicious config
// cannot read arbitrary files (e.g. /etc/passwd or ../../id_rsa) via the
// benchmark/comparator loaders.
func SafeReadPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty file path")
	}
	if filepath.IsAbs(p) {
		return "", fmt.Errorf("absolute file path is not allowed: %s", p)
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal is not allowed: %s", p)
	}
	return clean, nil
}

// SafeReadPathUnder validates a path that the tool itself derived from an
// operator-supplied root (e.g. walking the directory named by --from-jsonl).
// Absoluteness is fine here — what matters is that p stays inside root, so a
// symlink or a crafted entry cannot pull in a file from elsewhere. The returned
// path is absolute.
func SafeReadPathUnder(root, p string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("empty root path")
	}
	if p == "" {
		return "", fmt.Errorf("empty file path")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve root %s: %w", root, err)
	}
	absPath, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %w", p, err)
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return "", fmt.Errorf("%s is not under %s", p, root)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the corpus root %s: %s", root, p)
	}
	return absPath, nil
}
