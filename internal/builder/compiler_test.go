package builder

import (
	"os"
	"path/filepath"
	"testing"

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
