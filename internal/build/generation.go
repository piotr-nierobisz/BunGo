package build

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	bungo "github.com/piotr-nierobisz/BunGo"
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
func generateEmbeddedAssetsPackage(projectRoot string, modulePath string, webDirs []discoveredWebDir) (string, string, func(), error) {
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
		if info, statErr := os.Stat(sourceDir); statErr != nil || !info.IsDir() {
			continue
		}

		targetDir := filepath.Join(packageDir, filepath.FromSlash(webDir.embedPath))
		if copyErr := bungo.CopyFSTreeToDir(os.DirFS(sourceDir), ".", targetDir); copyErr != nil {
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
		source.WriteString("//go:embed ")
		source.WriteString(strings.Join(assetDirs, " "))
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
