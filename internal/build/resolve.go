package build

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// readModulePath extracts the Go module import path from go.mod.
// Inputs:
// - projectRoot: project root directory containing a go.mod file.
// Outputs:
// - string: module import path from the `module` directive.
// - error: non-nil when go.mod cannot be read or has no module declaration.
func readModulePath(projectRoot string) (string, error) {
	goModPath := filepath.Join(projectRoot, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "module ") {
			modulePath := strings.TrimSpace(strings.TrimPrefix(trimmed, "module "))
			if modulePath == "" {
				return "", errors.New("go.mod module declaration is empty")
			}
			return modulePath, nil
		}
	}

	return "", errors.New("go.mod module declaration not found")
}

// resolveEntryPackage resolves the build entry package directory and package name via go list.
// Inputs:
// - projectRoot: project root used as working directory for `go list`.
// - entry: package target passed to `go build`.
// Outputs:
// - string: absolute filesystem directory for the entry package.
// - string: Go package name for the entry package.
// - error: non-nil when go list fails or the target is not a main package.
func resolveEntryPackage(projectRoot string, entry string) (string, string, error) {
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}|{{.Name}}", entry)
	cmd.Dir = projectRoot
	output, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("resolving entry package failed: %w", err)
	}

	parts := strings.Split(strings.TrimSpace(string(output)), "|")
	if len(parts) != 2 {
		return "", "", errors.New("unexpected go list output while resolving entry package")
	}

	entryDir := strings.TrimSpace(parts[0])
	entryPkgName := strings.TrimSpace(parts[1])
	if entryPkgName != "main" {
		return "", "", fmt.Errorf("entry target %q is package %q, expected package main", entry, entryPkgName)
	}

	return entryDir, entryPkgName, nil
}

// resolveOutputPath returns an absolute output binary path and ensures the parent directory exists.
// Inputs:
// - projectRoot: project root used to resolve relative output paths.
// - entry: build entry target used to derive default binary name.
// - outputPath: optional explicit output path.
// Outputs:
// - string: absolute filesystem path where go build should write the binary.
// - error: non-nil when output directory creation fails.
func resolveOutputPath(projectRoot string, entry string, outputPath string) (string, error) {
	if strings.TrimSpace(outputPath) == "" {
		defaultBinaryName := filepath.Base(projectRoot)
		if entry != "." {
			defaultBinaryName = filepath.Base(filepath.Clean(entry))
		}
		outputPath = filepath.Join(projectRoot, "bin", defaultBinaryName)
	} else if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(projectRoot, outputPath)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return "", err
	}

	return outputPath, nil
}
