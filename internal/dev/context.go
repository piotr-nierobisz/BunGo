package dev

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveRunContext resolves the process working directory and go run target for dev mode.
// Inputs:
// - root: current project root directory where the CLI command is executed.
// - entry: user-provided --entry value.
// Outputs:
// - string: process working directory used by `go run`.
// - string: `go run` target argument.
// - error: non-nil when entry file targets cannot be resolved to an absolute path.
func ResolveRunContext(root, entry string) (string, string, error) {
	if entry == "" {
		return root, ".", nil
	}

	if strings.HasSuffix(strings.ToLower(entry), ".go") {
		absoluteEntry := entry
		if !filepath.IsAbs(absoluteEntry) {
			absoluteEntry = filepath.Join(root, entry)
		}
		absoluteEntry = filepath.Clean(absoluteEntry)
		if _, err := os.Stat(absoluteEntry); err == nil {
			return filepath.Dir(absoluteEntry), filepath.Base(absoluteEntry), nil
		} else if !os.IsNotExist(err) {
			return "", "", err
		}
	}

	return root, entry, nil
}
