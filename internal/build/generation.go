package build

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/piotr-nierobisz/BunGo/internal/theme"
)

// generateEmbeddedAssetsPackage creates a temporary package with copied web assets and embed registration.
// Inputs:
// - projectRoot: root directory where the temporary package directory is created.
// - modulePath: module import path used to construct the temporary package import path.
// - webDirs: discovered web directories with embed aliases and source disk paths.
// Outputs:
// - string: absolute path to the generated temporary package directory.
// - string: module import path for the generated package.
// - func(): cleanup callback removing generated package files.
// - error: non-nil when generation or directory copy operations fail.
func generateEmbeddedAssetsPackage(projectRoot, modulePath string, webDirs []discoveredWebDir) (string, string, func(), error) {
	packageDir, err := os.MkdirTemp(projectRoot, "bungo_embed_gen_")
	if err != nil {
		return "", "", func() {}, err
	}
	cleanup := func() {
		_ = os.RemoveAll(packageDir)
	}

	assetDirs := make([]string, 0, len(webDirs))
	for _, webDir := range webDirs {
		sourceDir := webDir.sourceDir
		info, statErr := os.Stat(sourceDir)
		if statErr != nil {
			cleanup()
			return "", "", func() {}, fmt.Errorf("discovered web dir %q is not accessible: %w", sourceDir, statErr)
		}
		if !info.IsDir() {
			cleanup()
			return "", "", func() {}, fmt.Errorf("discovered web dir %q is not a directory", sourceDir)
		}

		targetDir := filepath.Join(packageDir, filepath.FromSlash(webDir.embedPath))
		if copyErr := copyWebDirForEmbedding(sourceDir, targetDir); copyErr != nil {
			cleanup()
			return "", "", func() {}, copyErr
		}
		assetDirs = append(assetDirs, webDir.embedPath)
	}

	sort.Strings(assetDirs)
	importPath := modulePath + "/" + filepath.Base(packageDir)

	generatedFilePath := filepath.Join(packageDir, "embed_assets_gen.go")
	if err := writeEmbedPackageFile(generatedFilePath, assetDirs); err != nil {
		cleanup()
		return "", "", func() {}, err
	}

	return packageDir, importPath, cleanup, nil
}

// copyWebDirForEmbedding copies one web root into the generated package, deliberately leaving
// out every dot-prefixed file and directory (.DS_Store, .env, .git, editor swap files, ...).
// The generated go:embed directive uses the `all:` prefix so `_`-prefixed assets are embedded,
// which also turns off go:embed's own dotfile exclusion: this copy step is therefore the one
// place that keeps hidden files out of production binaries.
// Inputs:
// - sourceDir: web root directory on disk.
// - targetDir: destination directory inside the generated package.
// Outputs:
// - error: non-nil when walking, reading, or writing fails.
func copyWebDirForEmbedding(sourceDir string, targetDir string) error {
	sourceFS := os.DirFS(sourceDir)
	return fs.WalkDir(sourceFS, ".", func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if currentPath != "." && strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		targetPath := filepath.Join(targetDir, filepath.FromSlash(currentPath))
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}
		data, err := fs.ReadFile(sourceFS, currentPath)
		if err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0644)
	})
}

// writeEmbedPackageFile writes the temporary package source that registers embedded assets at init time.
// Inputs:
// - generatedFilePath: file path where generated package source should be written.
// - assetDirs: copied web directory names to include in go:embed directives.
// Outputs:
// - error: non-nil when source generation or file writing fails.
func writeEmbedPackageFile(generatedFilePath string, assetDirs []string) error {
	var source bytes.Buffer
	source.WriteString("package bungoembedgen\n\n")
	source.WriteString("import (\n")
	source.WriteString("\t\"embed\"\n\n")
	source.WriteString(fmt.Sprintf("\tbungo %q\n", theme.BunGoModuleImportPath))
	source.WriteString(")\n\n")

	if len(assetDirs) > 0 {
		// "all:" embeds `_`-prefixed files, which go:embed skips by default; dot-prefixed files
		// were already left out by copyWebDirForEmbedding. Quoting keeps a web dir whose name
		// contains spaces a single pattern.
		patterns := make([]string, 0, len(assetDirs))
		for _, assetDir := range assetDirs {
			patterns = append(patterns, strconv.Quote("all:"+assetDir))
		}
		source.WriteString("//go:embed ")
		source.WriteString(strings.Join(patterns, " "))
		source.WriteString("\n")
	}

	source.WriteString("var embeddedAssets embed.FS\n\n")
	source.WriteString("func init() {\n")
	source.WriteString("\tbungo.RegisterEmbeddedAssetsFS(embeddedAssets)\n")
	source.WriteString("}\n")

	return os.WriteFile(generatedFilePath, source.Bytes(), 0644)
}

// generateEntryImportFile writes a temporary blank import file in the entry package to force embed package linking.
// Inputs:
// - entryDir: absolute entry package directory where the temporary import file is created.
// - entryPkgName: package name for the entry directory, expected to be `main`.
// - embedImportPath: import path of the generated embed package.
// Outputs:
// - func(): cleanup callback that removes the generated import file.
// - error: non-nil when file generation fails.
func generateEntryImportFile(entryDir, entryPkgName, embedImportPath string) (func(), error) {
	importFilePath := filepath.Join(entryDir, "bungo_embed_import_gen.go")
	content := fmt.Sprintf(
		"package %s\n\nimport _ %q\n",
		entryPkgName,
		embedImportPath,
	)

	if err := os.WriteFile(importFilePath, []byte(content), 0644); err != nil {
		return func() {}, err
	}

	return func() {
		_ = os.Remove(importFilePath)
	}, nil
}
