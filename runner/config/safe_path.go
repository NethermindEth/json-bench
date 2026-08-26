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
// crafted entry cannot pull in a file from elsewhere. Symlinks are resolved
// first, since it is the target that gets read: a link inside the root pointing
// outside it would otherwise pass a purely lexical check. The returned path is
// absolute.
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

	// Compare the real locations when both resolve; a path that does not exist
	// yet, or a root reached through a link we cannot resolve, falls back to the
	// lexical comparison rather than failing the load.
	realRoot, rootErr := filepath.EvalSymlinks(absRoot)
	realPath, pathErr := filepath.EvalSymlinks(absPath)
	if rootErr == nil && pathErr == nil {
		absRoot, absPath = realRoot, realPath
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
