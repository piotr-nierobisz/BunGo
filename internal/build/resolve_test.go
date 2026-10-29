package build

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

func TestReadModulePath_formats(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		goMod   string
		want    string
		wantErr string
	}{
		{"leading comments and indentation", "// generated\n\n   module   example.com/bar  \n\ngo 1.25\n", "example.com/bar", ""},
		{"windows line endings", "module example.com/win\r\n\r\ngo 1.25\r\n", "example.com/win", ""},
		{"no module directive", "go 1.25\n\nrequire example.com/dep v1.0.0\n", "", "module declaration not found"},
		{"bare module keyword", "module\n", "", "module declaration not found"},
		{"keyword prefix of another word", "modulepath example.com/x\nmodule example.com/real\n", "example.com/real", ""},

		// go.mod syntax beyond a bare path.
		{"trailing comment", "module example.com/c // deprecated: use example.com/d\n", "example.com/c", ""},
		{"comment without space", "module example.com/c //comment\n", "example.com/c", ""},
		{"tab separator", "module\texample.com/tab\n", "example.com/tab", ""},
		{"interpreted string", "module \"example.com/q\"\n", "example.com/q", ""},
		{"interpreted string with escape and comment", "module \"example.com/\\u0071\" // quoted\n", "example.com/q", ""},
		{"raw string", "module `example.com/raw`\n", "example.com/raw", ""},
		{"quote directly after keyword", "module\"example.com/tight\"\n", "example.com/tight", ""},

		// Malformed declarations are reported instead of producing a broken import path.
		{"comment only", "module // nothing here\n", "", "module declaration is empty"},
		{"empty quoted path", "module \"\"\n", "", "module declaration is empty"},
		{"unterminated quote", "module \"example.com/oops\n", "", "not a valid quoted string"},
		{"text after path", "module example.com/a example.com/b\n", "", "unexpected text after the module path"},
		{"block syntax", "module (\n\texample.com/block\n)\n", "", "block syntax"},
	}
	for _, tt := range tests {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(tt.goMod), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := readModulePath(dir)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s: expected error containing %q, got %q, %v", tt.name, tt.wantErr, got, err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("%s: readModulePath = %q, %v; want %q", tt.name, got, err, tt.want)
		}
	}
}

func TestNormalizeBuildEntryTarget_cases(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tests := []struct {
		name        string
		projectRoot string
		entry       string
		entryDir    string
		want        string
		wantErr     string
	}{
		{"blank entry", root, "   ", root, ".", ""},
		{"package path passes through", root, "./cmd/server", filepath.Join(root, "cmd", "server"), "./cmd/server", ""},
		{"file in project root", root, "main.go", root, ".", ""},
		{"file in nested package", root, "cmd/server/main.go", filepath.Join(root, "cmd", "server"), "./cmd/server", ""},
		{"uppercase extension", root, "./cmd/server/MAIN.GO", filepath.Join(root, "cmd", "server"), "./cmd/server", ""},
		{"file outside project root", root, "../other/main.go", filepath.Join(filepath.Dir(root), "other"), "", "resolves outside project root"},
		{"relative root with absolute entry dir", "relative/root", "main.go", root, "", "normalizing file entry target failed"},
	}
	for _, tt := range tests {
		got, err := normalizeBuildEntryTarget(tt.projectRoot, tt.entry, tt.entryDir)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s: expected error containing %q, got %q, %v", tt.name, tt.wantErr, got, err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("%s: normalizeBuildEntryTarget = %q, %v; want %q", tt.name, got, err, tt.want)
		}
	}
}

func TestResolveOutputPath_cases(t *testing.T) {
	t.Setenv("GOOS", "linux") // default names below carry no executable suffix
	root := t.TempDir()
	absoluteOut := filepath.Join(t.TempDir(), "release", "app")
	tests := []struct {
		name   string
		entry  string
		output string
		want   string
	}{
		{"default name from package entry", "./cmd/api", "", filepath.Join(root, "bin", "api")},
		{"default name from file entry", "cmd/worker/worker.go", "  ", filepath.Join(root, "bin", "worker")},
		{"relative output under project root", ".", "dist/server", filepath.Join(root, "dist", "server")},
		{"absolute output kept as-is", ".", absoluteOut, absoluteOut},
	}
	for _, tt := range tests {
		got, err := resolveOutputPath(root, tt.entry, tt.output)
		if err != nil || got != tt.want {
			t.Errorf("%s: resolveOutputPath = %q, %v; want %q", tt.name, got, err, tt.want)
			continue
		}
		if info, err := os.Stat(filepath.Dir(got)); err != nil || !info.IsDir() {
			t.Errorf("%s: output directory %s was not created: %v", tt.name, filepath.Dir(got), err)
		}
	}
}

func TestResolveOutputPath_executableSuffixFollowsTargetOS(t *testing.T) {
	hostSuffix := ""
	if runtime.GOOS == "windows" {
		hostSuffix = ".exe"
	}
	tests := []struct {
		goos   string
		output string
		want   string
	}{
		{goos: "windows", want: filepath.Join("bin", "api.exe")},
		{goos: "linux", want: filepath.Join("bin", "api")},
		{goos: "darwin", want: filepath.Join("bin", "api")},
		{goos: "", want: filepath.Join("bin", "api"+hostSuffix)},                  // unset: host OS
		{goos: "windows", output: "dist/api", want: filepath.Join("dist", "api")}, // explicit output is kept as given
	}
	for _, tt := range tests {
		t.Setenv("GOOS", tt.goos)
		root := t.TempDir()
		got, err := resolveOutputPath(root, "./cmd/api", tt.output)
		if err != nil || got != filepath.Join(root, tt.want) {
			t.Errorf("GOOS=%q output=%q: resolveOutputPath = %q, %v; want %q", tt.goos, tt.output, got, err, filepath.Join(root, tt.want))
		}
	}
}

func TestResolveEntryPackage_pathWithPipe(t *testing.T) {
	requireHermeticGo(t)
	if runtime.GOOS == "windows" {
		t.Skip(`"|" is not allowed in Windows file names`)
	}
	root := filepath.Join(t.TempDir(), "team|app")
	writeTree(t, root, map[string]string{
		"go.mod":             "module example.com/app\n\ngo 1.22\n",
		"cmd/server/main.go": "package main\n\nfunc main() {}\n",
	})

	dir, name, err := resolveEntryPackage(root, "./cmd/server")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(root, "cmd", "server") || name != "main" {
		t.Fatalf("resolveEntryPackage = %q, %q", dir, name)
	}
}

func TestResolveEntryPackage_goToolchainMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, _, err := resolveEntryPackage(t.TempDir(), ".")
	if !errors.Is(err, exec.ErrNotFound) || !strings.HasPrefix(err.Error(), "resolving entry package failed: ") {
		t.Fatalf("expected a not-found error for the go command, got %v", err)
	}
}

func TestDecodeGoListPackages(t *testing.T) {
	t.Parallel()
	got, err := decodeGoListPackages([]byte("{\n\t\"Dir\": \"/p|q/a\",\n\t\"Name\": \"main\"\n}\n{\n\t\"Dir\": \"/p|q/b\",\n\t\"Name\": \"lib\"\n}\n"))
	if err != nil || len(got) != 2 || got[0] != (goListPackage{Dir: "/p|q/a", Name: "main"}) || got[1].Name != "lib" {
		t.Fatalf("decodeGoListPackages = %#v, %v", got, err)
	}
	if got, err := decodeGoListPackages(nil); err != nil || len(got) != 0 {
		t.Fatalf("empty output = %#v, %v", got, err)
	}
	if _, err := decodeGoListPackages([]byte(`{"Dir": "/a"`)); err == nil {
		t.Fatal("expected error for truncated go list output")
	}
}

func TestResolveOutputPath_parentIsFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bin"), []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveOutputPath(root, ".", ""); err == nil {
		t.Fatal("expected error when the bin output directory is a regular file")
	}
}
