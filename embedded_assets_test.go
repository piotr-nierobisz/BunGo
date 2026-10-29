package bungo

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRegisterEmbeddedAssetsFS_roundTrip(t *testing.T) {
	prev := getEmbeddedAssetsFS()
	defer RegisterEmbeddedAssetsFS(prev)

	mem := fstest.MapFS{"web/x.txt": &fstest.MapFile{Data: []byte("ok")}}
	RegisterEmbeddedAssetsFS(mem)
	got := getEmbeddedAssetsFS()
	if got == nil {
		t.Fatal("nil fs")
	}
	b, err := fs.ReadFile(got, "web/x.txt")
	if err != nil || string(b) != "ok" {
		t.Fatalf("read: %v %q", err, b)
	}
}

func TestNewServer_embeddedAssetsWithoutDiskWebDir(t *testing.T) {
	// A built binary ships its web tree embedded; the directory need not exist on disk.
	const webDir = "bungo-embedded-only-web"
	if _, err := os.Stat(webDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("precondition: %q must not exist on disk (stat err: %v)", webDir, err)
	}
	prev := getEmbeddedAssetsFS()
	t.Cleanup(func() { RegisterEmbeddedAssetsFS(prev) })
	RegisterEmbeddedAssetsFS(fstest.MapFS{
		webDir + "/layouts/home.gohtml": &fstest.MapFile{Data: []byte(`<html><head></head><body>{{.Title}}</body></html>`)},
		webDir + "/views/home.jsx":      &fstest.MapFile{Data: []byte("export default function Home(){return null}")},
	})

	s := NewServer(&noopEngine{}, webDir) // must not panic: layouts/ and views/ exist in memory
	s.Page(PageRoute{Path: "/", Template: "home.gohtml", View: "home.jsx"})

	html, err := RenderTemplate(s.AssetStorage(), "layouts/home.gohtml", "", "", "", map[string]any{"Title": "embedded"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "<body>embedded</body>") {
		t.Fatalf("render from embedded assets: %s", html)
	}

	// esbuild needs real files: the embedded tree is materialized into a temp dir that cleanup removes.
	setTempDir(t, t.TempDir())
	buildDir, cleanup, err := s.AssetStorage().PrepareWebDirForBuild()
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(buildDir, "views", "home.jsx")); err != nil || !strings.Contains(string(b), "Home") {
		t.Fatalf("materialized view: %q, %v", b, err)
	}
	cleanup()
	if _, err := os.Stat(buildDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("cleanup must remove the materialized tree, stat err: %v", err)
	}
}
