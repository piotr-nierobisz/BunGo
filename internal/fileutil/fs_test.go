package fileutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestCopyFSTreeToDir(t *testing.T) {
	t.Parallel()
	mapFS := fstest.MapFS{
		"web/layouts/a.gohtml": &fstest.MapFile{Data: []byte("<html></html>")},
		"web/views/x.jsx":      &fstest.MapFile{Data: []byte("export default function X(){return null}")},
		"web/static/app.css":   &fstest.MapFile{Data: []byte("body{}")},
	}

	dir := t.TempDir()
	if err := CopyFSTreeToDir(mapFS, "web", dir); err != nil {
		t.Fatalf("CopyFSTreeToDir: %v", err)
	}

	for _, rel := range []string{"layouts/a.gohtml", "views/x.jsx", "static/app.css"} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if len(b) == 0 {
			t.Fatalf("empty file %s", rel)
		}
	}
}

func TestCopyFSTreeToDir_emptyRootDot(t *testing.T) {
	t.Parallel()
	mapFS := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("x")},
	}
	dir := t.TempDir()
	if err := CopyFSTreeToDir(mapFS, ".", dir); err != nil {
		t.Fatalf("CopyFSTreeToDir: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "x" {
		t.Fatalf("content = %q", b)
	}
}

func TestCopyFSTreeToDir_rootDotKeepsLeadingDots(t *testing.T) {
	t.Parallel()
	mapFS := fstest.MapFS{
		".env":        &fstest.MapFile{Data: []byte("env")},
		".git/config": &fstest.MapFile{Data: []byte("git")},
		"views/.keep": &fstest.MapFile{Data: []byte("keep")},
	}
	dir := t.TempDir()
	if err := CopyFSTreeToDir(mapFS, ".", dir); err != nil {
		t.Fatalf("CopyFSTreeToDir: %v", err)
	}
	for rel, want := range map[string]string{".env": "env", ".git/config": "git", "views/.keep": "keep"} {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(b) != want {
			t.Fatalf("%s content = %q, want %q", rel, b, want)
		}
	}
	for _, stripped := range []string{"env", "git"} {
		if _, err := os.Stat(filepath.Join(dir, stripped)); err == nil {
			t.Fatalf("%s was written with its leading dot stripped", stripped)
		}
	}
}

func TestCopyFSTreeToDir_walkError(t *testing.T) {
	t.Parallel()
	bad := &errorFS{err: fs.ErrNotExist}
	err := CopyFSTreeToDir(bad, "missing", t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
}

type errorFS struct {
	err error
}

func (e *errorFS) Open(name string) (fs.File, error) {
	return nil, e.err
}
