package fileutil

import (
	"path"
	"path/filepath"
	"strings"
)

// NormalizeSlashPath normalizes separators to forward slashes and trims surrounding whitespace.
// Inputs:
// - raw: path-like string that may contain platform separators or leading/trailing spaces.
// Outputs:
// - string: normalized slash-style path string.
func NormalizeSlashPath(raw string) string {
	return strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
}

// CleanRelativeSlashPath normalizes and validates a slash-style relative path.
// Inputs:
// - raw: path-like string relative to a logical root; optional leading "/" or "./" is tolerated.
// Outputs:
// - string: cleaned slash-style path guaranteed not to escape root.
// - bool: true when the cleaned path is valid and relative.
func CleanRelativeSlashPath(raw string) (string, bool) {
	normalized := NormalizeSlashPath(raw)
	normalized = strings.TrimPrefix(normalized, "/")
	normalized = strings.TrimPrefix(normalized, "./")
	if normalized == "" {
		return "", false
	}

	cleaned := path.Clean(normalized)
	if cleaned == "" || cleaned == "." || cleaned == "/" || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

// CleanProjectRelativePath normalizes and validates a project-relative path.
// Inputs:
// - raw: path-like string expected to stay within a project root.
// Outputs:
// - string: cleaned slash-style project-relative path.
// - bool: true when the path is valid, non-empty, and not absolute.
func CleanProjectRelativePath(raw string) (string, bool) {
	normalized := NormalizeSlashPath(raw)
	if normalized == "" || strings.HasPrefix(normalized, "/") {
		return "", false
	}
	normalized = strings.TrimPrefix(normalized, "./")

	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(normalized)))
	if cleaned == "" || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

// JoinRootAndSlashPath joins a filesystem root with a slash-style relative path.
// Inputs:
// - root: absolute or relative filesystem root path.
// - relativePath: slash-style relative path that should be joined under root.
// Outputs:
// - string: cleaned filesystem path joined under root.
func JoinRootAndSlashPath(root string, relativePath string) string {
	return filepath.Clean(filepath.Join(root, filepath.FromSlash(relativePath)))
}
