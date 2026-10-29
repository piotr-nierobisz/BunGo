package build

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeWebDirForEmbedding(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if wd, ok := normalizeWebDirForEmbedding("./sub", root); !ok {
		t.Fatal("expected ok")
	} else if filepath.Clean(wd.sourceDir) != filepath.Join(root, "sub") {
		t.Fatalf("source %q", wd.sourceDir)
	}
	if _, ok := normalizeWebDirForEmbedding("/abs", root); ok {
		t.Fatal("absolute should fail")
	}
	if _, ok := normalizeWebDirForEmbedding("", root); ok {
		t.Fatal("empty should fail")
	}
}

func TestResolveManualWebDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	web := filepath.Join(root, "assets", "web")
	if err := os.MkdirAll(filepath.Join(web, "layouts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(web, "views"), 0755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveManualWebDir(root, "./assets/web")
	if err != nil || len(got) != 1 || got[0].embedPath != "assets/web" {
		t.Fatalf("%v %#v", err, got)
	}

	_, err = resolveManualWebDir(root, "")
	if err == nil {
		t.Fatal("empty manual path")
	}
}

func TestDiscoverServerWebDirs_fromAST(t *testing.T) {
	root := t.TempDir()
	entryDir := filepath.Join(root, "cmd", "app")
	if err := os.MkdirAll(entryDir, 0755); err != nil {
		t.Fatal(err)
	}
	webDir := filepath.Join(root, "web")
	for _, sub := range []string{"layouts", "views"} {
		if err := os.MkdirAll(filepath.Join(webDir, sub), 0755); err != nil {
			t.Fatal(err)
		}
	}

	src := `package main

import bungo "github.com/piotr-nierobisz/BunGo"

func main() {
	_ = bungo.NewServer(nil, "./web")
}
`
	if err := os.WriteFile(filepath.Join(entryDir, "main.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	dirs, err := discoverServerWebDirs(root, entryDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 || dirs[0].embedPath != "web" {
		t.Fatalf("%#v", dirs)
	}
	if filepath.Clean(dirs[0].sourceDir) != filepath.Clean(webDir) {
		t.Fatalf("want %s got %s", webDir, dirs[0].sourceDir)
	}
}

func TestNormalizeWebDirForEmbedding_cases(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tests := []struct {
		in        string
		wantEmbed string
		wantOK    bool
	}{
		{"web", "web", true},
		{"  ./web  ", "web", true},
		{`assets\site`, "assets/site", true},
		{"assets/../site", "site", true},
		{"../outside", "", false},
		{"./", "", false},
		{"/abs/web", "", false},
		{"   ", "", false},
	}
	for _, tt := range tests {
		got, ok := normalizeWebDirForEmbedding(tt.in, root)
		if ok != tt.wantOK || got.embedPath != tt.wantEmbed {
			t.Errorf("normalizeWebDirForEmbedding(%q) = (%q, %v); want (%q, %v)", tt.in, got.embedPath, ok, tt.wantEmbed, tt.wantOK)
			continue
		}
		if ok && got.sourceDir != filepath.Join(root, filepath.FromSlash(tt.wantEmbed)) {
			t.Errorf("normalizeWebDirForEmbedding(%q) source = %q", tt.in, got.sourceDir)
		}
	}
}

func TestResolveManualWebDir_cases(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "assets", "web"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "site.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()

	tests := []struct {
		name        string
		projectRoot string
		webDir      string
		wantEmbed   string
		wantErr     string
	}{
		{"absolute path inside root", root, filepath.Join(root, "assets", "web"), "assets/web", ""},
		{"windows separators", root, `assets\web`, "assets/web", ""},
		{"whitespace only", root, "   ", "", "cannot be empty"},
		{"absolute path is the root itself", root, root, "", "must stay inside project root"},
		{"absolute path outside root", root, outside, "", "must stay inside project root"},
		{"relative path escaping root", root, "../elsewhere", "", "must be a valid project-relative path"},
		{"missing directory", root, "assets/missing", "", "is not accessible"},
		{"regular file", root, "site.txt", "", "is not a directory"},
		{"relative project root with absolute web dir", "relative/root", outside, "", "resolving manual web dir"},
	}
	for _, tt := range tests {
		got, err := resolveManualWebDir(tt.projectRoot, tt.webDir)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s: expected error containing %q, got %v (%#v)", tt.name, tt.wantErr, err, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		wantSource := filepath.Join(root, filepath.FromSlash(tt.wantEmbed))
		if len(got) != 1 || got[0].embedPath != tt.wantEmbed || got[0].sourceDir != wantSource {
			t.Errorf("%s: got %#v; want embed %q source %q", tt.name, got, tt.wantEmbed, wantSource)
		}
	}
}

func TestDiscoverServerWebDirs_collectsAndFiltersCalls(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	entryDir := filepath.Join(root, "cmd", "app")
	writeTree(t, entryDir, map[string]string{
		"main.go": `package main

import bungo "github.com/piotr-nierobisz/BunGo"

const dynamicDir = "./from-const"

func NewServer(engine any, dir string) {}

func main() {
	_ = bungo.NewServer(nil, "./web")
	_ = bungo.NewServer(nil, "web")          // same directory, different spelling
	_ = bungo.NewServer(nil, ` + "`admin/site`" + `) // raw string literal
	_ = bungo.NewServer(nil, "/srv/abs/web") // absolute: never embedded
	_ = bungo.NewServer(nil, "../outside")   // escapes project root
	_ = bungo.NewServer(nil, dynamicDir)     // package-level constant
	_ = bungo.NewServer(nil, os.Getenv("WEB_DIR")) // only known at runtime
	_ = bungo.NewServer(nil, 8080)           // not a string literal
	_ = bungo.NewServer("only-one-argument")
	_ = bungo.NewRouter(nil, "./not-a-server")
	NewServer(nil, "./unqualified-call")
}
`,
		"internal/admin/routes.go": `package admin

import bungo "github.com/piotr-nierobisz/BunGo"

func Mount() { _ = bungo.NewServer(nil, "admin/site") }
`,
		".hidden/skip.go": `package hidden

import bungo "github.com/piotr-nierobisz/BunGo"

func f() { _ = bungo.NewServer(nil, "./hidden-web") }
`,
		// Files that cannot hold a BunGo server call never fail discovery, even when they are not valid Go.
		"notes.go":  "this is not Go source",
		"README.md": `bungo.NewServer(nil, "./docs-web") github.com/piotr-nierobisz/BunGo`,
	})

	dirs, err := discoverServerWebDirs(root, entryDir)
	if err != nil {
		t.Fatal(err)
	}
	assertDiscovered(t, root, dirs, "admin/site", "from-const", "web")
}

// assertDiscovered checks discovered web dirs against embed paths, each sourced under root.
func assertDiscovered(t *testing.T, root string, dirs []discoveredWebDir, wantEmbedPaths ...string) {
	t.Helper()
	var got []string
	for _, dir := range dirs {
		got = append(got, dir.embedPath)
		if want := filepath.Join(root, filepath.FromSlash(dir.embedPath)); dir.sourceDir != want {
			t.Errorf("%s: source dir = %q; want %q", dir.embedPath, dir.sourceDir, want)
		}
	}
	if strings.Join(got, ",") != strings.Join(wantEmbedPaths, ",") {
		t.Fatalf("discovered %v; want %v", got, wantEmbedPaths)
	}
}

func TestDiscoverServerWebDirs_onlyBunGoServerCalls(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		// A different package's NewServer in the same file must not become an embed root.
		// The unaliased import binds BunGo's package name, bungo.
		"main.go": `package main

import (
	"github.com/piotr-nierobisz/BunGo"
	"github.com/piotr-nierobisz/BunGo/engine"
	"example.com/sse"
)

var factories = map[string]func(any, string) any{}

func main() {
	_ = sse.NewServer(nil, ":8080")
	_ = engine.NewServer(nil, "./engine-arg")
	_ = factories["custom"](nil, "./factory-web")
	_ = bungo.NewServer(engine.NewHTTPEngine(), "./web")
}
`,
		"aliased/server.go": `package aliased

import bg "github.com/piotr-nierobisz/BunGo"

func New() { _ = bg.NewServer(nil, "./aliased-web") }
`,
		"dotted/server.go": `package dotted

import . "github.com/piotr-nierobisz/BunGo"

func New() { _ = NewServer(nil, "./dotted-web") }
`,
		// A blank import binds no name, so bungo.NewServer here is some other identifier.
		"blank/server.go": `package blank

import _ "github.com/piotr-nierobisz/BunGo"

func New() { _ = bungo.NewServer(nil, "./blank-web") }
`,
		// Tests are not part of the built binary.
		"main_test.go": `package main

import bungo "github.com/piotr-nierobisz/BunGo"

func helper() { _ = bungo.NewServer(nil, "./testdata/web") }
`,
	})

	dirs, err := discoverServerWebDirs(root, root)
	if err != nil {
		t.Fatal(err)
	}
	assertDiscovered(t, root, dirs, "aliased-web", "dotted-web", "web")
}

func TestDiscoverServerWebDirs_resolvesPackageConstants(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"main.go": `package main

import bungo "github.com/piotr-nierobisz/BunGo"

func main() {
	_ = bungo.NewServer(nil, webDir)
	_ = bungo.NewServer(nil, adminDir)
	_ = bungo.NewServer(nil, (docsDir))
	_ = bungo.NewServer(nil, repeated)
	_ = bungo.NewServer(nil, typedDir)
	_ = bungo.NewServer(nil, assetsRoot+"/"+"site")
	_ = bungo.NewServer(nil, unknownDir+"/web")
	_ = bungo.NewServer(nil, "./"+unknownDir)
	_ = bungo.NewServer(nil, "./a" == "./b")
	_ = bungo.NewServer(nil, loopA)
}
`,
		// Constants usually live next to main, in a file that never mentions BunGo.
		"config.go": `package main

const webDir = "./web"

const (
	assetsRoot = "assets"
	adminDir   = assetsRoot + "/admin"
	docsDir    = "docs"
	first      = "./implicit"
	repeated
)

const typedDir string = "typed"

// Cyclic constants do not compile, but discovery runs before compilation and must not hang.
const loopA = loopB
const loopB = loopA
`,
		// Another package's constant with the same name is not visible to main.
		"other/config.go": `package other

const webDir = "./wrong-package"
`,
	})

	dirs, err := discoverServerWebDirs(root, root)
	if err != nil {
		t.Fatal(err)
	}
	assertDiscovered(t, root, dirs, "assets/admin", "assets/site", "docs", "implicit", "typed", "web")
}

func TestDiscoverServerWebDirs_entryInsideDotDirectory(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), ".workspace")
	writeTree(t, root, map[string]string{
		"main.go": "package main\n\nimport bungo \"github.com/piotr-nierobisz/BunGo\"\n\nfunc main() { _ = bungo.NewServer(nil, \"./site\") }\n",
	})
	dirs, err := discoverServerWebDirs(root, root)
	if err != nil {
		t.Fatal(err)
	}
	// Only dot-directories below the entry are skipped, never the entry itself.
	assertDiscovered(t, root, dirs, "site")
}

func TestDiscoverServerWebDirs_fallbackWebDir(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		files      map[string]string // relative to project root
		wantSource string            // relative to project root; empty means no web dir
	}{
		{"prefers entry web dir", map[string]string{"cmd/app/web/views/.keep": "", "web/views/.keep": ""}, "cmd/app/web"},
		{"falls back to project web dir", map[string]string{"web/views/.keep": ""}, "web"},
		{"ignores entry web file", map[string]string{"cmd/app/web": "file", "web/views/.keep": ""}, "web"},
		{"no web dir anywhere", map[string]string{"cmd/app/.keep": ""}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTree(t, root, tt.files)
			writeTree(t, root, map[string]string{"cmd/app/main.go": "package main\n\nfunc main() {}\n"})

			dirs, err := discoverServerWebDirs(root, filepath.Join(root, "cmd", "app"))
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantSource == "" {
				if len(dirs) != 0 {
					t.Fatalf("expected no web dirs, got %#v", dirs)
				}
				return
			}
			want := filepath.Join(root, filepath.FromSlash(tt.wantSource))
			if len(dirs) != 1 || dirs[0].embedPath != "web" || dirs[0].sourceDir != want {
				t.Fatalf("got %#v; want embed web from %s", dirs, want)
			}
		})
	}
}

func TestDiscoverServerWebDirs_errors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if _, err := discoverServerWebDirs(root, filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing entry dir: expected not-exist error, got %v", err)
	}

	writeTree(t, root, map[string]string{
		"cmd/app/main.go": "package main\n\nimport bungo \"github.com/piotr-nierobisz/BunGo\"\n\nfunc main() { bungo.NewServer(nil, \"./web\" }\n",
	})
	if _, err := discoverServerWebDirs(root, filepath.Join(root, "cmd", "app")); err == nil || !strings.Contains(err.Error(), "main.go:5:") {
		t.Fatalf("expected parse error for main.go, got %v", err)
	}
}
