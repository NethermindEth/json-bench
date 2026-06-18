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
