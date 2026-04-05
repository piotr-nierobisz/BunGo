package build

import (
	"os"
	"path/filepath"
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
