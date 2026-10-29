package build

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/piotr-nierobisz/BunGo/internal/theme"
)

// parseGenerated parses a generated Go file and returns its AST plus every comment line.
func parseGenerated(t *testing.T, path string) (*ast.File, []string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	if err != nil {
		src, _ := os.ReadFile(path)
		t.Fatalf("generated file %s does not parse: %v\n%s", path, err, src)
	}
	var comments []string
	for _, group := range file.Comments {
		for _, c := range group.List {
			comments = append(comments, c.Text)
		}
	}
	return file, comments
}

func importPaths(t *testing.T, file *ast.File) []string {
	t.Helper()
	var paths []string
	for _, spec := range file.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

func assertNoEmbedPackages(t *testing.T, projectRoot string) {
	t.Helper()
	if matches, _ := filepath.Glob(filepath.Join(projectRoot, "bungo_embed_gen_*")); len(matches) != 0 {
		t.Fatalf("generated package not cleaned up after failure: %v", matches)
	}
}

func TestGenerateEmbeddedAssetsPackage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"web/layouts/base.gohtml":     "<html></html>",
		"web/views/home.jsx":          "export default 1",
		"admin/site/views/panel.jsx":  "export default 2",
		"admin/site/static/admin.css": "body{}",
	})

	packageDir, importPath, cleanup, err := generateEmbeddedAssetsPackage(root, "example.com/app", []discoveredWebDir{
		{embedPath: "web", sourceDir: filepath.Join(root, "web")},
		{embedPath: "admin/site", sourceDir: filepath.Join(root, "admin", "site")},
	})
	if err != nil {
		t.Fatal(err)
	}

	if filepath.Dir(packageDir) != root || !strings.HasPrefix(filepath.Base(packageDir), "bungo_embed_gen_") {
		t.Fatalf("package dir %q should be a bungo_embed_gen_ directory directly under the project root", packageDir)
	}
	if want := "example.com/app/" + filepath.Base(packageDir); importPath != want {
		t.Fatalf("import path = %q; want %q", importPath, want)
	}

	for rel, want := range map[string]string{
		"web/layouts/base.gohtml":     "<html></html>",
		"web/views/home.jsx":          "export default 1",
		"admin/site/views/panel.jsx":  "export default 2",
		"admin/site/static/admin.css": "body{}",
	} {
		got, err := os.ReadFile(filepath.Join(packageDir, filepath.FromSlash(rel)))
		if err != nil || string(got) != want {
			t.Errorf("copied asset %s = %q, %v; want %q", rel, got, err, want)
		}
	}

	file, comments := parseGenerated(t, filepath.Join(packageDir, "embed_assets_gen.go"))
	if file.Name.Name != "bungoembedgen" {
		t.Fatalf("package name = %q", file.Name.Name)
	}
	if got := strings.Join(importPaths(t, file), ","); got != "embed,"+theme.BunGoModuleImportPath {
		t.Fatalf("imports = %s", got)
	}
	// "all:" keeps `_`-prefixed assets; quoting keeps each web dir one pattern.
	if !containsString(comments, `//go:embed "all:admin/site" "all:web"`) {
		t.Fatalf("expected sorted, quoted all: go:embed directive, comments: %q", comments)
	}

	cleanup()
	if _, err := os.Stat(packageDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup did not remove %s: %v", packageDir, err)
	}
}

// listTree returns every regular file under root as slash paths, sorted.
func listTree(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestCopyWebDirForEmbedding_skipsDotfilesKeepsUnderscoreFiles(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	writeTree(t, source, map[string]string{
		"layouts/base.gohtml":     "<html></html>",
		"layouts/_partial.gohtml": "partial",
		"static/_app.css":         "body{}",
		"_drafts/post.html":       "draft",
		".DS_Store":               "finder",
		"static/.env":             "SECRET=1",
		".git/config":             "[core]",
		"views/.cache/x.js":       "cached",
	})
	target := filepath.Join(t.TempDir(), "copy")

	if err := copyWebDirForEmbedding(source, target); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(listTree(t, target), ",")
	want := "_drafts/post.html,layouts/_partial.gohtml,layouts/base.gohtml,static/_app.css"
	if got != want {
		t.Fatalf("copied files = %s; want %s", got, want)
	}
}

func TestCopyWebDirForEmbedding_errors(t *testing.T) {
	t.Parallel()
	if err := copyWebDirForEmbedding(filepath.Join(t.TempDir(), "missing"), t.TempDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source: expected not-exist error, got %v", err)
	}

	source := t.TempDir()
	if err := os.Symlink(filepath.Join(source, "gone.css"), filepath.Join(source, "dangling.css")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := copyWebDirForEmbedding(source, t.TempDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dangling symlink: expected not-exist error, got %v", err)
	}
}

func TestWriteEmbedPackageFile_quotesPatterns(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "embed_assets_gen.go")
	if err := writeEmbedPackageFile(path, []string{"my site", `odd"name`}); err != nil {
		t.Fatal(err)
	}
	_, comments := parseGenerated(t, path)
	if !containsString(comments, `//go:embed "all:my site" "all:odd\"name"`) {
		t.Fatalf("expected each web dir as one quoted pattern, comments: %q", comments)
	}
}

func TestGenerateEmbeddedAssetsPackage_errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string) (projectRoot string, dirs []discoveredWebDir)
		wantErr string
		wantIs  error
	}{
		{
			name: "project root missing",
			setup: func(t *testing.T, root string) (string, []discoveredWebDir) {
				return filepath.Join(root, "missing"), nil
			},
			wantIs: os.ErrNotExist,
		},
		{
			name: "web dir missing",
			setup: func(t *testing.T, root string) (string, []discoveredWebDir) {
				return root, []discoveredWebDir{{embedPath: "web", sourceDir: filepath.Join(root, "web")}}
			},
			wantErr: "is not accessible",
		},
		{
			name: "web dir is a file",
			setup: func(t *testing.T, root string) (string, []discoveredWebDir) {
				writeTree(t, root, map[string]string{"web": "not a directory"})
				return root, []discoveredWebDir{{embedPath: "web", sourceDir: filepath.Join(root, "web")}}
			},
			wantErr: "is not a directory",
		},
		{
			name: "copy target collides with a previously copied file",
			setup: func(t *testing.T, root string) (string, []discoveredWebDir) {
				writeTree(t, root, map[string]string{
					"site/nested":      "a file where the second web dir wants a directory",
					"other/views/x.js": "x",
				})
				return root, []discoveredWebDir{
					{embedPath: "site", sourceDir: filepath.Join(root, "site")},
					{embedPath: "site/nested", sourceDir: filepath.Join(root, "other")},
				}
			},
			wantErr: "not a directory",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			projectRoot, dirs := tt.setup(t, root)

			packageDir, importPath, cleanup, err := generateEmbeddedAssetsPackage(projectRoot, "example.com/app", dirs)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) || (tt.wantIs != nil && !errors.Is(err, tt.wantIs)) {
				t.Fatalf("expected error containing %q (is %v), got %v", tt.wantErr, tt.wantIs, err)
			}
			if packageDir != "" || importPath != "" || cleanup == nil {
				t.Fatalf("failure should return empty paths and a no-op cleanup: %q %q %v", packageDir, importPath, cleanup == nil)
			}
			cleanup()
			assertNoEmbedPackages(t, root)
		})
	}
}

func TestWriteEmbedPackageFile_withoutAssets(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "embed_assets_gen.go")
	if err := writeEmbedPackageFile(path, nil); err != nil {
		t.Fatal(err)
	}
	_, comments := parseGenerated(t, path)
	for _, c := range comments {
		if strings.HasPrefix(c, "//go:embed") {
			t.Fatalf("go:embed with no patterns does not compile; got directive %q", c)
		}
	}
}

func TestWriteEmbedPackageFile_writeError(t *testing.T) {
	t.Parallel()
	err := writeEmbedPackageFile(filepath.Join(t.TempDir(), "missing", "gen.go"), []string{"web"})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected not-exist error, got %v", err)
	}
}

func TestGenerateEntryImportFile(t *testing.T) {
	t.Parallel()
	entryDir := t.TempDir()
	cleanup, err := generateEntryImportFile(entryDir, "main", "example.com/app/bungo_embed_gen_123")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(entryDir, "bungo_embed_import_gen.go")
	file, _ := parseGenerated(t, path)
	if file.Name.Name != "main" {
		t.Fatalf("package = %q", file.Name.Name)
	}
	if len(file.Imports) != 1 || file.Imports[0].Name == nil || file.Imports[0].Name.Name != "_" {
		t.Fatalf("expected exactly one blank import, got %#v", file.Imports)
	}
	if got := importPaths(t, file)[0]; got != "example.com/app/bungo_embed_gen_123" {
		t.Fatalf("import path = %q", got)
	}

	cleanup()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup did not remove import file: %v", err)
	}
	cleanup() // removing an already-removed file must be harmless
}

func TestGenerateEntryImportFile_missingEntryDir(t *testing.T) {
	t.Parallel()
	cleanup, err := generateEntryImportFile(filepath.Join(t.TempDir(), "missing"), "main", "example.com/app/x")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected not-exist error, got %v", err)
	}
	cleanup()
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
