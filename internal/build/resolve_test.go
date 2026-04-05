package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadModulePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n\ngo 1.25\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := readModulePath(dir)
	if err != nil || got != "example.com/foo" {
		t.Fatalf("readModulePath: %v %q", err, got)
	}
}

func TestReadModulePath_missing(t *testing.T) {
	t.Parallel()
	_, err := readModulePath(t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDeriveDefaultBinaryName(t *testing.T) {
	t.Parallel()
	if got := deriveDefaultBinaryName("/proj/root", ""); got != "root" {
		t.Fatalf("empty entry: %q", got)
	}
	if got := deriveDefaultBinaryName("/p", "./cmd/main.go"); got != "main" {
		t.Fatalf("file entry: %q", got)
	}
	if got := deriveDefaultBinaryName("/p", "./server"); got != "server" {
		t.Fatalf("dir entry: %q", got)
	}
}

func TestNormalizeBuildEntryTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	got, err := normalizeBuildEntryTarget(root, "", root)
	if err != nil || got != "." {
		t.Fatalf("%v %q", err, got)
	}
}

func TestResolveOutputPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	out, err := resolveOutputPath(root, ".", "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(out) != filepath.Join(root, "bin") {
		t.Fatalf("unexpected dir: %s", out)
	}
}
