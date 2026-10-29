package build

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/piotr-nierobisz/BunGo/internal/theme"
)

// requireHermeticGo skips tests that shell out to `go list` / `go build` when the toolchain is
// unavailable or -short is set, and pins an offline, reproducible environment for those child
// processes (no go.work from the developer's checkout, no module proxy, no toolchain downloads).
func requireHermeticGo(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("runs the go toolchain; skipped in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not found on PATH")
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("CGO_ENABLED", "0")
}

// stubBunGoSource stands in for the real BunGo module in fixture projects. It provides just the
// API the build pipeline relies on: NewServer (discovered in user code) and RegisterEmbeddedAssetsFS
// (called by the generated embed package), so fixtures compile in about a second without network.
const stubBunGoSource = `package bungo

import "io/fs"

var Embedded fs.FS

func RegisterEmbeddedAssetsFS(assets fs.FS) { Embedded = assets }

type Server struct{ WebDir string }

func NewServer(engine any, webDir string) *Server { return &Server{WebDir: webDir} }
`

// mainListingEmbeddedFiles returns an entry package that boots a server on webDir and prints every
// file of the embedded filesystem registered by the generated package, one "path=contents" per line.
func mainListingEmbeddedFiles(webDir string) string {
	return fmt.Sprintf(`package main

import (
	"fmt"
	"io/fs"
	"os"

	bungo "github.com/piotr-nierobisz/BunGo"
)

func main() {
	_ = bungo.NewServer(nil, %q)
	if bungo.Embedded == nil {
		fmt.Fprintln(os.Stderr, "no embedded assets registered")
		os.Exit(1)
	}
	err := fs.WalkDir(bungo.Embedded, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(bungo.Embedded, path)
		fmt.Printf("%%s=%%s\n", path, data)
		return err
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`, webDir)
}

// runBinary executes a built fixture binary and returns its stdout.
func runBinary(t *testing.T, binary string) string {
	t.Helper()
	stdout, err := exec.Command(binary).Output()
	if err != nil {
		t.Fatalf("running built binary: %v", err)
	}
	return string(stdout)
}

// hostExecutable appends the executable suffix go build uses on the host.
func hostExecutable(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// writeFixtureProject creates a module wired to the stub BunGo module plus the given files.
func writeFixtureProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.22\n\n" +
			"require github.com/piotr-nierobisz/BunGo v0.0.0\n\n" +
			"replace github.com/piotr-nierobisz/BunGo => ./third_party/bungo\n",
		"third_party/bungo/go.mod":   "module github.com/piotr-nierobisz/BunGo\n\ngo 1.22\n",
		"third_party/bungo/bungo.go": stubBunGoSource,
	})
	writeTree(t, root, files)
	return root
}

func assertNoGeneratedLeftovers(t *testing.T, projectRoot string, entryDir string) {
	t.Helper()
	if matches, _ := filepath.Glob(filepath.Join(projectRoot, "bungo_embed_gen_*")); len(matches) != 0 {
		t.Errorf("generated embed package left behind: %v", matches)
	}
	if _, err := os.Stat(filepath.Join(entryDir, "bungo_embed_import_gen.go")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("generated import file left behind in %s (stat err: %v)", entryDir, err)
	}
}

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiEscape.ReplaceAllString(s, "") }

// assertInOrder fails unless every want string appears in got, in the given order.
func assertInOrder(t *testing.T, got string, wants ...string) {
	t.Helper()
	offset := 0
	for _, want := range wants {
		idx := strings.Index(got[offset:], want)
		if idx < 0 {
			t.Fatalf("missing %q (in order) in output:\n%s", want, got)
		}
		offset += idx + len(want)
	}
}

type stageRecorder struct {
	stages  []string
	details []string
}

func (r *stageRecorder) record(stage string, detail string) {
	r.stages = append(r.stages, stage)
	r.details = append(r.details, detail)
}

func TestRun_buildsSelfContainedBinary(t *testing.T) {
	requireHermeticGo(t)
	root := writeFixtureProject(t, map[string]string{
		"main.go":                  mainListingEmbeddedFiles("./web"),
		"web/layouts/base.gohtml":  "<html></html>",
		"web/views/hello.txt":      "hello from the embedded web root",
		"web/static/_partial.css":  "underscore asset",
		"web/_includes/footer.txt": "underscore dir",
		"web/.DS_Store":            "finder metadata",
		"web/static/.env":          "SECRET=1",
		"web/.git/config":          "[core]",
	})

	var out bytes.Buffer
	binary, err := Run(&out, root, ".", "", "")
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	if want := filepath.Join(root, "bin", hostExecutable(filepath.Base(root))); binary != want {
		t.Fatalf("binary path = %q; want %q", binary, want)
	}
	assertNoGeneratedLeftovers(t, root, root)

	// The produced binary must serve assets from memory, not from the source tree. `_`-prefixed
	// files and directories are embedded; dot-prefixed ones never reach the binary.
	if err := os.RemoveAll(filepath.Join(root, "web")); err != nil {
		t.Fatal(err)
	}
	want := "web/_includes/footer.txt=underscore dir\n" +
		"web/layouts/base.gohtml=<html></html>\n" +
		"web/static/_partial.css=underscore asset\n" +
		"web/views/hello.txt=hello from the embedded web root\n"
	if got := runBinary(t, binary); got != want {
		t.Fatalf("embedded files:\n%s\nwant:\n%s", got, want)
	}

	log := stripANSI(out.String())
	assertInOrder(t, log,
		theme.EN.Build.PipelineTitle,
		fmt.Sprintf(theme.EN.Build.MetaFmt, ".", theme.EN.Build.OutputDefault),
		theme.EN.Build.StepReadGoMod,
		theme.EN.Build.StepEntry,
		theme.EN.Build.StepOutput,
		theme.EN.Build.StepDiscover,
		theme.EN.Build.StepGenerate,
		theme.EN.Build.StepLink,
		theme.EN.Build.StepCompile,
		theme.EN.Build.Separator,
		fmt.Sprintf(theme.EN.Build.SuccessFmt, binary),
	)
	if steps := strings.Count(log, "•"); steps != 7 {
		t.Fatalf("expected 7 printed steps (the internal done stage is hidden), got %d:\n%s", steps, log)
	}
}

func TestExecuteBuildPipeline_fileEntryWithManualWebDir(t *testing.T) {
	requireHermeticGo(t)
	root := writeFixtureProject(t, map[string]string{
		// The NewServer literal points at a directory that does not exist: the --web-dir flag must
		// bypass discovery entirely, otherwise asset generation would fail.
		"cmd/server/main.go":                 mainListingEmbeddedFiles("./not-used"),
		"assets/my site/layouts/base.gohtml": "<html></html>",
		"assets/my site/views/page.txt":      "served from the manual web dir",
	})

	var rec stageRecorder
	// A .go file entry must be normalized to its package so the generated import file is compiled too.
	// The web dir name contains a space, which must stay one go:embed pattern.
	binary, err := executeBuildPipeline(root, "cmd/server/main.go", "dist/server-bin", "assets/my site", rec.record)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "dist", "server-bin"); binary != want {
		t.Fatalf("binary path = %q; want %q", binary, want)
	}
	wantStages := []string{"module", "entry", "output", "discover", "generate", "link", "compile", "done"}
	if strings.Join(rec.stages, ",") != strings.Join(wantStages, ",") {
		t.Fatalf("stages = %v; want %v", rec.stages, wantStages)
	}
	if last := rec.details[len(rec.details)-1]; last != "server-bin" {
		t.Fatalf("done detail = %q; want binary name", last)
	}
	assertNoGeneratedLeftovers(t, root, filepath.Join(root, "cmd", "server"))

	want := "assets/my site/layouts/base.gohtml=<html></html>\n" +
		"assets/my site/views/page.txt=served from the manual web dir\n"
	if got := runBinary(t, binary); got != want {
		t.Fatalf("embedded files:\n%s\nwant:\n%s", got, want)
	}
}

func TestRun_compileFailureCleansUpGeneratedFiles(t *testing.T) {
	requireHermeticGo(t)
	mainSource := `package main

import bungo "github.com/piotr-nierobisz/BunGo"

func main() {
	_ = bungo.NewServer(nil, "./web")
	var port int = "not a number"
	_ = port
}
`
	root := writeFixtureProject(t, map[string]string{
		"main.go":                 mainSource,
		"web/layouts/base.gohtml": "<html></html>",
		"web/views/.keep":         "",
	})

	var out bytes.Buffer
	binary, err := Run(&out, root, ".", "", "")
	if err == nil {
		t.Fatalf("expected go build to fail, got binary %q", binary)
	}
	if binary != "" {
		t.Fatalf("failed build returned binary path %q", binary)
	}
	assertNoGeneratedLeftovers(t, root, root)
	if src, _ := os.ReadFile(filepath.Join(root, "main.go")); string(src) != mainSource {
		t.Fatal("user entry file was modified")
	}

	log := stripANSI(out.String())
	if !strings.Contains(log, theme.EN.Build.StepCompile) || strings.Contains(log, theme.EN.Build.Separator) {
		t.Fatalf("expected progress up to compile and no success output:\n%s", log)
	}
}

func TestRun_missingGoMod(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if _, err := Run(&out, t.TempDir(), "", "", ""); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing go.mod error, got %v", err)
	}
	log := stripANSI(out.String())
	assertInOrder(t, log, theme.EN.Build.PipelineTitle, theme.EN.Build.StepReadGoMod)
	if strings.Contains(log, theme.EN.Build.StepEntry) {
		t.Fatalf("pipeline should stop after reading go.mod:\n%s", log)
	}
}

func TestExecuteBuildPipeline_failsBeforeCompiling(t *testing.T) {
	requireHermeticGo(t)
	const mainWithWeb = `package main

import bungo "github.com/piotr-nierobisz/BunGo"

func main() { _ = bungo.NewServer(nil, "./web") }
`
	tests := []struct {
		name         string
		files        map[string]string
		entry        string
		output       string
		manualWebDir string
		wantErr      string
		wantLast     string
	}{
		{
			name:     "entry is not package main",
			files:    map[string]string{"lib/lib.go": "package lib\n"},
			entry:    "./lib",
			wantErr:  `is package "lib", expected package main`,
			wantLast: "entry",
		},
		{
			name:     "entry does not exist",
			files:    map[string]string{"main.go": mainWithWeb},
			entry:    "./does-not-exist",
			wantErr:  "resolving entry package failed",
			wantLast: "entry",
		},
		{
			name: "entry pattern matches several packages",
			files: map[string]string{
				"main.go":          mainWithWeb,
				"cmd/tool/main.go": "package main\n\nfunc main() {}\n",
			},
			entry:    "./...",
			wantErr:  `entry target "./..." matches 2 packages, expected exactly one main package`,
			wantLast: "entry",
		},
		{
			name: "file entry outside project root",
			files: map[string]string{
				"main.go":            mainWithWeb,
				"../outside/main.go": "package main\n\nfunc main() {}\n",
			},
			entry:    "../outside/main.go",
			wantErr:  "resolves outside project root",
			wantLast: "entry",
		},
		{
			name:     "output parent is a regular file",
			files:    map[string]string{"main.go": mainWithWeb, "web/views/.keep": "", "blocker": "x"},
			entry:    ".",
			output:   "blocker/app",
			wantErr:  "not a directory",
			wantLast: "output",
		},
		{
			name:         "manual web dir escapes project",
			files:        map[string]string{"main.go": mainWithWeb},
			entry:        ".",
			manualWebDir: "../elsewhere",
			wantErr:      "must be a valid project-relative path",
			wantLast:     "discover",
		},
		{
			name: "unparseable server source in entry tree",
			files: map[string]string{
				"main.go":             mainWithWeb,
				"tools/broken/srv.go": "package broken\n\nimport bungo \"github.com/piotr-nierobisz/BunGo\"\n\nfunc f() { bungo.NewServer(nil, \"./web\" }\n",
			},
			entry:    ".",
			wantErr:  filepath.Join("tools", "broken", "srv.go") + ":5:",
			wantLast: "discover",
		},
		{
			name:     "web dir referenced by NewServer is missing",
			files:    map[string]string{"main.go": mainWithWeb},
			entry:    ".",
			wantErr:  "is not accessible",
			wantLast: "generate",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeFixtureProject(t, tt.files)
			var rec stageRecorder
			_, err := executeBuildPipeline(root, tt.entry, tt.output, tt.manualWebDir, rec.record)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
			if last := rec.stages[len(rec.stages)-1]; last != tt.wantLast {
				t.Fatalf("pipeline stopped at stage %q; want %q (stages %v)", last, tt.wantLast, rec.stages)
			}
			assertNoGeneratedLeftovers(t, root, root)
		})
	}
}
