package bungo

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestNewAssetStorage_diskOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustMk(t, filepath.Join(dir, "layouts"), filepath.Join(dir, "views"))

	s := newAssetStorage(dir, nil)
	if !s.Exists("") {
		t.Fatal("expected web root exists")
	}
	if !s.Exists("layouts") {
		t.Fatal("expected layouts")
	}
	data := []byte("hello")
	mustWrite(t, filepath.Join(dir, "layouts", "p.gohtml"), data)
	got, err := s.ReadFile("layouts/p.gohtml")
	if err != nil || string(got) != "hello" {
		t.Fatalf("ReadFile: %v, %q", err, got)
	}
	if _, err := s.ReadStaticFile("../x"); err == nil {
		t.Fatal("ReadStaticFile should fail traversal")
	}
	_, err = s.ReadFile("../etc/passwd")
	if err == nil {
		t.Fatal("expected error for traversal")
	}
}

func TestNewAssetStorage_embeddedPreference(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustMk(t, filepath.Join(dir, "layouts"), filepath.Join(dir, "views"))

	mem := fstest.MapFS{
		"web/layouts/p.gohtml": &fstest.MapFile{Data: []byte("from-mem")},
	}
	_ = os.WriteFile(filepath.Join(dir, "layouts", "p.gohtml"), []byte("from-disk"), 0644)

	s := newAssetStorage("web", mem)
	got, err := s.ReadFile("layouts/p.gohtml")
	if err != nil || string(got) != "from-mem" {
		t.Fatalf("expected memory first: %v %q", err, got)
	}
}

func TestAssetStorage_nil(t *testing.T) {
	t.Parallel()
	var s *AssetStorage
	if s.Exists("x") {
		t.Fatal("nil Exists")
	}
	if _, err := s.ReadFile("x"); err == nil {
		t.Fatal("nil ReadFile")
	}
	if _, _, err := s.PrepareWebDirForBuild(); err != nil {
		t.Fatal(err)
	}
}

func TestAssetStorage_PrepareWebDirForBuild_embedded(t *testing.T) {
	t.Parallel()
	mem := fstest.MapFS{
		"web/layouts/a.gohtml": &fstest.MapFile{Data: []byte("<html/>")},
		"web/views/v.jsx":      &fstest.MapFile{Data: []byte("export default function V(){return null}")},
	}
	s := newAssetStorage("web", mem)
	webDir, cleanup, err := s.PrepareWebDirForBuild()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if webDir == "" {
		t.Fatal("empty webDir")
	}
	b, err := os.ReadFile(filepath.Join(webDir, "layouts", "a.gohtml"))
	if err != nil || string(b) != "<html/>" {
		t.Fatalf("materialized: %v %q", err, b)
	}
}

func TestAssetStorage_ReadStaticFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	staticDir := filepath.Join(dir, "static")
	mustMk(t, filepath.Join(dir, "layouts"), filepath.Join(dir, "views"), staticDir)
	mustWrite(t, filepath.Join(staticDir, "x.css"), []byte("body{}"))
	s := newAssetStorage(dir, nil)
	b, err := s.ReadStaticFile("x.css")
	if err != nil || string(b) != "body{}" {
		t.Fatalf("%v %q", err, b)
	}
}

func mustMk(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestNewAssetStorage_invalidMemoryRoot(t *testing.T) {
	t.Parallel()
	// webDir that does not clean to a valid embed root skips memory
	s := newAssetStorage("../outside", fstest.MapFS{})
	if s.memoryFS != nil {
		t.Fatal("expected no memory fs for invalid root")
	}
}
