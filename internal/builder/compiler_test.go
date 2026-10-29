package builder

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	bungo "github.com/piotr-nierobisz/BunGo"
)

func TestCompilePages_noViews(t *testing.T) {
	t.Parallel()
	compiled, optimized, err := CompilePages(map[string]bungo.PageRoute{
		"/": {Path: "/", Template: "x.gohtml"},
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled) != 0 || len(optimized) != 0 {
		t.Fatalf("expected empty maps: %d %d", len(compiled), len(optimized))
	}
}

func TestCompilePagesFromStorage_nil(t *testing.T) {
	t.Parallel()
	c, o, err := CompilePagesFromStorage(nil, nil)
	if err != nil || len(c) != 0 || len(o) != 0 {
		t.Fatalf("%v %v %v", err, c, o)
	}
}

func TestCompilePages_exampleViews(t *testing.T) {
	webDir := filepath.Join("..", "..", "examples", "http_web", "web")
	if st, err := os.Stat(webDir); err != nil || !st.IsDir() {
		t.Skip("example web dir not available")
	}
	pages := map[string]bungo.PageRoute{
		"/": {Path: "/", Template: "landing.gohtml", View: "landing.jsx"},
	}
	compiled, optimized, err := CompilePages(pages, webDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled) == 0 || len(optimized) == 0 {
		t.Fatal("expected compiled output")
	}
	if _, ok := compiled["landing.jsx"]; !ok {
		t.Fatal("missing landing.jsx entry")
	}
}

func TestCompilePages_missingViewReportsEsbuildError(t *testing.T) {
	t.Parallel()
	_, _, err := CompilePages(map[string]bungo.PageRoute{
		"/": {Path: "/", Template: "x.gohtml", View: "missing.jsx"},
	}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "esbuild errors") {
		t.Fatalf("expected esbuild error, got %v", err)
	}
}

func TestCompilePages_sharedAndNestedViews(t *testing.T) {
	t.Parallel()
	webDir := t.TempDir()
	writeView(t, webDir, "home.jsx", `function Home() { return "home-view-marker"; }
_bungoRender(Home);
`)
	writeView(t, webDir, "admin/panel.tsx", `const label: string = "panel-view-marker";
function Panel() { return label; }
_bungoRender(Panel, "panel-root");
`)

	compiled, optimized, err := CompilePages(map[string]bungo.PageRoute{
		"/":      {Path: "/", Template: "home.gohtml", View: "home.jsx"},
		"/home":  {Path: "/home", Template: "home.gohtml", View: "home.jsx"},
		"/admin": {Path: "/admin", Template: "admin.gohtml", View: "admin/panel.tsx"},
	}, webDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled) != 2 || len(optimized) != 2 {
		t.Fatalf("expected one bundle per unique view, got compiled=%d optimized=%d", len(compiled), len(optimized))
	}

	home, panel := compiled["home.jsx"], compiled["admin/panel.tsx"]
	if !strings.Contains(home, "home-view-marker") || strings.Contains(home, "panel-view-marker") {
		t.Fatal("home bundle should contain only the home view")
	}
	if !strings.Contains(panel, "panel-view-marker") || strings.Contains(panel, "home-view-marker") {
		t.Fatal("panel bundle should contain only the panel view")
	}
	if !strings.Contains(panel, "panel-root") {
		t.Fatal("panel bundle should keep the custom mount element id")
	}

	for view, js := range compiled {
		key := bungo.OptimizedAssetPath(view, js)
		if optimized[key] != js {
			t.Errorf("optimized map missing %s for view %s", key, view)
		}
	}
	if key := bungo.OptimizedAssetPath("admin/panel.tsx", panel); !strings.HasPrefix(key, "/_bungo/admin/panel.") {
		t.Errorf("nested view should keep its directory in the optimized path, got %s", key)
	}
}

func TestCompilePages_viewsDifferingOnlyByExtension(t *testing.T) {
	t.Parallel()
	webDir := t.TempDir()
	writeView(t, webDir, "dash.jsx", `_bungoRender(() => "jsx-dashboard-marker");
`)
	writeView(t, webDir, "dash.tsx", `const label: string = "tsx-dashboard-marker";
_bungoRender(() => label);
`)

	compiled, optimized, err := CompilePages(map[string]bungo.PageRoute{
		"/legacy": {Path: "/legacy", Template: "dash.gohtml", View: "dash.jsx"},
		"/next":   {Path: "/next", Template: "dash.gohtml", View: "dash.tsx"},
	}, webDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled["dash.jsx"], "jsx-dashboard-marker") || strings.Contains(compiled["dash.jsx"], "tsx-dashboard-marker") {
		t.Fatal("dash.jsx must get its own bundle")
	}
	if !strings.Contains(compiled["dash.tsx"], "tsx-dashboard-marker") || strings.Contains(compiled["dash.tsx"], "jsx-dashboard-marker") {
		t.Fatal("dash.tsx must get its own bundle, not a copy of dash.jsx")
	}
	if len(optimized) != 2 {
		t.Fatalf("expected two distinct optimized bundles, got %d", len(optimized))
	}
	for view, js := range compiled {
		if optimized[bungo.OptimizedAssetPath(view, js)] != js {
			t.Errorf("optimized bundle for %s is not served at its content-hashed path", view)
		}
	}
}

func TestCompilePagesFromStorage_diskWebDir(t *testing.T) {
	t.Parallel()
	webDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(webDir, "layouts"), 0755); err != nil {
		t.Fatal(err)
	}
	writeView(t, webDir, "app.jsx", `function App() { return "disk-view-marker"; }
_bungoRender(App);
`)
	server := bungo.NewServer(nil, webDir)

	compiled, optimized, err := CompilePagesFromStorage(map[string]bungo.PageRoute{
		"/": {Path: "/", Template: "app.gohtml", View: "app.jsx"},
	}, server.AssetStorage())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled["app.jsx"], "disk-view-marker") || len(optimized) != 1 {
		t.Fatalf("unexpected output: compiled=%v optimized=%d", compiled, len(optimized))
	}
}

// useEmbeddedAssets registers a process-wide embedded FS, as a `bungo build` binary does at init.
// Callers must not run in parallel because the registration is global.
func useEmbeddedAssets(t *testing.T, assets fs.FS) {
	t.Helper()
	bungo.RegisterEmbeddedAssetsFS(assets)
	t.Cleanup(func() { bungo.RegisterEmbeddedAssetsFS(nil) })
}

func TestCompilePagesFromStorage_extractsEmbeddedAssets(t *testing.T) {
	tmp := t.TempDir()
	// PrepareWebDirForBuild extracts embedded views beneath os.TempDir().
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	useEmbeddedAssets(t, fstest.MapFS{
		"web/layouts/base.gohtml": {Data: []byte("<html></html>")},
		"web/views/app.jsx":       {Data: []byte("function App() { return \"embedded-view-marker\"; }\n_bungoRender(App);\n")},
	})
	// "web" does not exist on disk next to this package, so the views can only come from memory.
	server := bungo.NewServer(nil, "web")

	compiled, _, err := CompilePagesFromStorage(map[string]bungo.PageRoute{
		"/": {Path: "/", Template: "base.gohtml", View: "app.jsx"},
	}, server.AssetStorage())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled["app.jsx"], "embedded-view-marker") {
		t.Fatal("expected bundle compiled from embedded view")
	}
	if leftovers, _ := os.ReadDir(tmp); len(leftovers) != 0 {
		t.Fatalf("temporary extraction directory was not cleaned up: %v", leftovers)
	}
}

var errInjectedRead = errors.New("injected read failure")

// failingOpenFS serves a MapFS but fails to open one specific file.
// It deliberately exposes only Open so fs helpers cannot bypass the failure.
type failingOpenFS struct {
	base     fstest.MapFS
	failPath string
}

func (f failingOpenFS) Open(name string) (fs.File, error) {
	if name == f.failPath {
		return nil, &fs.PathError{Op: "open", Path: name, Err: errInjectedRead}
	}
	return f.base.Open(name)
}

func TestCompilePagesFromStorage_extractionError(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	useEmbeddedAssets(t, failingOpenFS{
		base: fstest.MapFS{
			"web/layouts/base.gohtml": {Data: []byte("<html></html>")},
			"web/views/app.jsx":       {Data: []byte("_bungoRender(() => null);")},
		},
		failPath: "web/views/app.jsx",
	})
	server := bungo.NewServer(nil, "web")

	_, _, err := CompilePagesFromStorage(map[string]bungo.PageRoute{
		"/": {Path: "/", Template: "base.gohtml", View: "app.jsx"},
	}, server.AssetStorage())
	if !errors.Is(err, errInjectedRead) {
		t.Fatalf("expected extraction error to propagate, got %v", err)
	}
}
