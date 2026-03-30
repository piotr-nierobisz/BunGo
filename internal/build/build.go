package build

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/piotr-nierobisz/BunGo/internal/theme"
)

// ProgressFunc receives build stage updates emitted by Run.
type ProgressFunc func(stage string, detail string)

// Run builds a BunGo project binary and renders styled progress output for CLI execution.
// Inputs:
// - out: destination writer receiving formatted progress and final success lines.
// - projectRoot: absolute project root directory where `go.mod` is located.
// - entry: `go build` entry target, such as `.`, `./cmd/server`, or `cmd/server/main.go`.
// - outputPath: optional explicit output path; empty uses `./bin/<entry-name>`.
// - manualWebDir: optional web root supplied by CLI `--web-dir`; when non-empty auto-discovery is skipped.
// Outputs:
// - string: absolute path to the produced binary.
// - error: non-nil when discovery, generation, build execution, or cleanup setup fails.
func Run(out io.Writer, projectRoot string, entry string, outputPath string, manualWebDir string) (string, error) {
	printer := newProgressPrinter(out, entry, outputPath)
	printer.start()

	binaryPath, err := executeBuildPipeline(projectRoot, entry, outputPath, manualWebDir, printer.step)
	if err != nil {
		return "", err
	}

	printer.success(binaryPath)
	fmt.Fprintln(out, lipgloss.NewStyle().Foreground(theme.Success).Bold(true).Render(fmt.Sprintf(theme.EN.Build.SuccessFmt, binaryPath)))
	return binaryPath, nil
}

// executeBuildPipeline executes the BunGo build pipeline and optionally reports progress.
// Inputs:
// - projectRoot: absolute project root directory where `go.mod` is located.
// - entry: `go build` entry target, such as `.`, `./cmd/server`, or `cmd/server/main.go`.
// - outputPath: optional explicit output path; empty uses `./bin/<entry-name>`.
// - manualWebDir: optional web root supplied by CLI `--web-dir`; when non-empty auto-discovery is skipped.
// - progress: optional callback receiving stage and detail messages during build execution.
// Outputs:
// - string: absolute path to the produced binary.
// - error: non-nil when discovery, generation, build execution, or cleanup setup fails.
func executeBuildPipeline(projectRoot, entry, outputPath string, manualWebDir string, progress ProgressFunc) (string, error) {
	if entry == "" {
		entry = "."
	}

	progress("module", theme.EN.Build.StepReadGoMod)
	modulePath, err := readModulePath(projectRoot)
	if err != nil {
		return "", err
	}

	progress("entry", theme.EN.Build.StepEntry)
	entryDir, entryPkgName, err := resolveEntryPackage(projectRoot, entry)
	if err != nil {
		return "", err
	}
	normalizedBuildTarget, err := normalizeBuildEntryTarget(projectRoot, entry, entryDir)
	if err != nil {
		return "", err
	}

	progress("output", theme.EN.Build.StepOutput)
	resolvedOutputPath, err := resolveOutputPath(projectRoot, entry, outputPath)
	if err != nil {
		return "", err
	}

	progress("discover", theme.EN.Build.StepDiscover)
	var webDirs []discoveredWebDir
	if manualWebDir != "" {
		webDirs, err = resolveManualWebDir(projectRoot, manualWebDir)
		if err != nil {
			return "", err
		}
	} else {
		webDirs, err = discoverServerWebDirs(projectRoot, entryDir)
		if err != nil {
			return "", err
		}
	}

	progress("generate", theme.EN.Build.StepGenerate)
	embedPackageDir, embedImportPath, cleanupGeneratedAssets, err := generateEmbeddedAssetsPackage(projectRoot, modulePath, webDirs)
	if err != nil {
		return "", err
	}
	defer cleanupGeneratedAssets()

	progress("link", theme.EN.Build.StepLink)
	cleanupImportFile, err := generateEntryImportFile(entryDir, entryPkgName, embedImportPath)
	if err != nil {
		return "", err
	}
	defer cleanupImportFile()

	progress("compile", theme.EN.Build.StepCompile)
	if err := runGoBuild(projectRoot, normalizedBuildTarget, resolvedOutputPath); err != nil {
		return "", err
	}

	_ = embedPackageDir
	progress("done", filepath.Base(resolvedOutputPath))
	return resolvedOutputPath, nil
}
