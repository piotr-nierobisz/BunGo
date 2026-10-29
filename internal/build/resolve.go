package build

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode"

	"github.com/piotr-nierobisz/BunGo/internal/fileutil"
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
		if value, ok := cutModuleKeyword(strings.TrimSpace(line)); ok {
			return parseModuleDirectiveValue(value)
		}
	}

	return "", errors.New("go.mod module declaration not found")
}

// cutModuleKeyword returns the text after a leading `module` keyword on a trimmed go.mod line.
// Inputs:
// - line: go.mod line with surrounding whitespace removed.
// Outputs:
// - string: the remainder after `module`, trimmed.
// - bool: true when the line is a module directive (keyword followed by whitespace or a quote).
func cutModuleKeyword(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, "module")
	if !ok || rest == "" || !strings.ContainsRune(" \t\"`", rune(rest[0])) {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// parseModuleDirectiveValue extracts the module path from a module directive, following go.mod
// syntax: the path may be a bare token or an interpreted ("...") or raw (`...`) Go string, and
// may be followed by a // comment.
// Inputs:
// - value: trimmed text after the `module` keyword.
// Outputs:
// - string: unquoted module path.
// - error: non-nil when the path is missing, malformed, uses block syntax, or has trailing text.
func parseModuleDirectiveValue(value string) (string, error) {
	var modulePath, trailing string
	switch {
	case strings.HasPrefix(value, `"`) || strings.HasPrefix(value, "`"):
		quoted, err := strconv.QuotedPrefix(value)
		if err != nil {
			return "", fmt.Errorf("go.mod module declaration %q is not a valid quoted string", value)
		}
		// Unquote cannot fail on a prefix QuotedPrefix accepted.
		modulePath, _ = strconv.Unquote(quoted)
		trailing = value[len(quoted):]
	case !strings.HasPrefix(value, "//"):
		end := strings.IndexFunc(value, unicode.IsSpace)
		if end < 0 {
			end = len(value)
		}
		modulePath, trailing = value[:end], value[end:]
	}

	if trailing = strings.TrimSpace(trailing); trailing != "" && !strings.HasPrefix(trailing, "//") {
		return "", fmt.Errorf("go.mod module declaration has unexpected text after the module path: %q", trailing)
	}
	switch modulePath {
	case "":
		return "", errors.New("go.mod module declaration is empty")
	case "(":
		return "", errors.New("go.mod module block syntax `module ( ... )` is not supported; use `module <path>`")
	}
	return modulePath, nil
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
	// JSON output keeps directories containing any character (including "|") intact.
	cmd := exec.Command("go", "list", "-json=Dir,Name", entry)
	cmd.Dir = projectRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		// go list explains failures (unknown package, syntax errors, ...) on stderr only.
		if reason := strings.TrimSpace(stderr.String()); reason != "" {
			return "", "", fmt.Errorf("resolving entry package failed: %w: %s", err, reason)
		}
		return "", "", fmt.Errorf("resolving entry package failed: %w", err)
	}

	packages, err := decodeGoListPackages(output)
	if err != nil {
		return "", "", fmt.Errorf("unexpected go list output while resolving entry package: %w", err)
	}
	if len(packages) != 1 {
		return "", "", fmt.Errorf("entry target %q matches %d packages, expected exactly one main package", entry, len(packages))
	}

	entryDir := packages[0].Dir
	entryPkgName := packages[0].Name
	if entryPkgName != "main" {
		return "", "", fmt.Errorf("entry target %q is package %q, expected package main", entry, entryPkgName)
	}

	return entryDir, entryPkgName, nil
}

// goListPackage holds the `go list -json` fields used to resolve the entry package.
type goListPackage struct {
	Dir  string
	Name string
}

// decodeGoListPackages decodes the stream of JSON objects printed by `go list -json`.
// Inputs:
// - output: raw stdout of `go list -json`, one object per matched package.
// Outputs:
// - []goListPackage: decoded packages in output order.
// - error: non-nil when the output is not a valid JSON object stream.
func decodeGoListPackages(output []byte) ([]goListPackage, error) {
	decoder := json.NewDecoder(bytes.NewReader(output))
	var packages []goListPackage
	for decoder.More() {
		var pkg goListPackage
		if err := decoder.Decode(&pkg); err != nil {
			return nil, err
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

// normalizeBuildEntryTarget converts a user entry into a package target safe for `go build`.
// Inputs:
// - projectRoot: project root directory used as the relative boundary for package targets.
// - entry: original user-provided `--entry` value.
// - entryDir: absolute filesystem directory returned by resolveEntryPackage for the entry package.
// Outputs:
// - string: normalized package-style build target (for example `.` or `./cmd/server`).
// - error: non-nil when entryDir cannot be represented relative to projectRoot.
func normalizeBuildEntryTarget(projectRoot string, entry string, entryDir string) (string, error) {
	trimmedEntry := strings.TrimSpace(entry)
	if trimmedEntry == "" {
		return ".", nil
	}
	if !strings.HasSuffix(strings.ToLower(trimmedEntry), ".go") {
		return entry, nil
	}

	relativeEntryDir, err := filepath.Rel(projectRoot, entryDir)
	if err != nil {
		return "", fmt.Errorf("normalizing file entry target failed: %w", err)
	}
	relativeEntryDir = fileutil.NormalizeSlashPath(relativeEntryDir)
	if relativeEntryDir == "." {
		return ".", nil
	}
	cleanedRelativeDir, ok := fileutil.CleanProjectRelativePath(relativeEntryDir)
	if !ok {
		return "", fmt.Errorf("file entry %q resolves outside project root", entry)
	}
	return "./" + strings.TrimPrefix(cleanedRelativeDir, "./"), nil
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
		defaultBinaryName := deriveDefaultBinaryName(projectRoot, entry) + targetExecutableSuffix()
		outputPath = filepath.Join(projectRoot, "bin", defaultBinaryName)
	} else if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(projectRoot, outputPath)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return "", err
	}

	return outputPath, nil
}

// targetExecutableSuffix returns the file suffix go build uses for executables of the target OS.
// The GOOS environment variable (which the child go build inherits) takes precedence over the host OS.
// Inputs:
// - none
// Outputs:
// - string: ".exe" when targeting Windows, otherwise empty.
func targetExecutableSuffix() string {
	goos := os.Getenv("GOOS")
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "windows" {
		return ".exe"
	}
	return ""
}

// deriveDefaultBinaryName derives the default binary name from the build entry target.
// Inputs:
// - projectRoot: project root path used as fallback naming source.
// - entry: user-provided build entry target.
// Outputs:
// - string: default binary file name without directory components.
func deriveDefaultBinaryName(projectRoot string, entry string) string {
	if strings.TrimSpace(entry) == "" || entry == "." {
		return filepath.Base(projectRoot)
	}

	cleanEntry := filepath.Clean(entry)
	baseName := filepath.Base(cleanEntry)
	if strings.HasSuffix(strings.ToLower(baseName), ".go") {
		withoutExt := strings.TrimSuffix(baseName, filepath.Ext(baseName))
		if strings.TrimSpace(withoutExt) != "" {
			return withoutExt
		}
	}
	return baseName
}
