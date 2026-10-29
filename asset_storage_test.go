package bungo

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/piotr-nierobisz/BunGo/internal/fileutil"
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
	// layouts/p.gohtml exists in the web root, so only the static-dir containment check
	// can refuse this; a missing target would pass even with traversal broken.
	if b, err := s.ReadStaticFile("../layouts/p.gohtml"); err == nil {
		t.Fatalf("ReadStaticFile escaped static/ and read %q", b)
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

var errUnreadable = errors.New("embedded file unreadable")

// unreadableFS wraps an fs.FS and fails to open one path, simulating a corrupt embedded asset.
// It deliberately exposes only Open so fs.ReadFile and fs.Stat cannot bypass the failure.
type unreadableFS struct {
	base fs.FS
	fail string
}

func (u unreadableFS) Open(name string) (fs.File, error) {
	if name == u.fail {
		return nil, &fs.PathError{Op: "open", Path: name, Err: errUnreadable}
	}
	return u.base.Open(name)
}

// setTempDir points os.TempDir (and therefore os.MkdirTemp("", ...)) at dir for the test's duration.
func setTempDir(t *testing.T, dir string) {
	t.Helper()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, dir)
	}
}

func TestAssetStorage_Exists_diskAndTraversal(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	web := filepath.Join(parent, "web")
	mustMk(t, filepath.Join(web, "layouts"))
	mustWrite(t, filepath.Join(web, "layouts", "p.gohtml"), []byte("x"))
	// A real file just outside the web root: no relative path may reach it.
	mustWrite(t, filepath.Join(parent, "secret.txt"), []byte("s"))

	s := newAssetStorage(web, nil)
	tests := []struct {
		path string
		want bool
	}{
		{"", true},
		{"layouts", true},
		{"layouts/p.gohtml", true},
		{"/layouts/p.gohtml", true},
		{"layouts/missing.gohtml", false},
		{"../secret.txt", false},
		{"layouts/../../secret.txt", false},
	}
	for _, tt := range tests {
		if got := s.Exists(tt.path); got != tt.want {
			t.Errorf("Exists(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}

	if newAssetStorage(filepath.Join(parent, "absent"), nil).Exists("") {
		t.Error("a missing web root must not be reported as existing")
	}
}

func TestAssetStorage_noWebDirNeverReadsWorkingDirectory(t *testing.T) {
	t.Parallel()
	// The package test binary runs in the module root, where go.mod exists. Storage
	// without a web dir must not resolve relative paths against the process cwd.
	if _, err := os.Stat("go.mod"); err != nil {
		t.Fatalf("precondition: go.mod should exist in the test working directory: %v", err)
	}
	s := newAssetStorage("", nil)
	if s.Exists("go.mod") {
		t.Error("Exists resolved a path against the working directory")
	}
	if _, err := s.ReadFile("go.mod"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadFile without web dir: got %v, want fs.ErrNotExist", err)
	}
}

func TestAssetStorage_embeddedWithDiskFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustMk(t, filepath.Join(dir, "layouts"))
	mustWrite(t, filepath.Join(dir, "layouts", "disk.gohtml"), []byte("from-disk"))
	mustWrite(t, filepath.Join(dir, "layouts", "both.gohtml"), []byte("disk-copy"))

	// newAssetStorage looks up the embedded tree under the cleaned webDir path.
	root, ok := fileutil.CleanRelativeSlashPath(dir)
	if !ok {
		t.Fatalf("temp dir %q does not clean to an embed root", dir)
	}
	mem := fstest.MapFS{
		root + "/layouts/mem.gohtml":  &fstest.MapFile{Data: []byte("from-mem")},
		root + "/layouts/both.gohtml": &fstest.MapFile{Data: []byte("mem-copy")},
	}
	s := newAssetStorage(dir, mem)
	if s.memoryFS == nil {
		t.Fatal("embedded fs should be active for a web dir present in it")
	}

	for _, p := range []string{"", "layouts/mem.gohtml", "layouts/disk.gohtml"} {
		if !s.Exists(p) {
			t.Errorf("Exists(%q) = false, want true", p)
		}
	}
	if s.Exists("layouts/none.gohtml") {
		t.Error("Exists reported a file missing from both stores")
	}

	reads := map[string]string{
		"layouts/both.gohtml": "mem-copy",  // memory wins over disk
		"layouts/disk.gohtml": "from-disk", // disk fallback for files not embedded
		"layouts/mem.gohtml":  "from-mem",
	}
	for p, want := range reads {
		got, err := s.ReadFile(p)
		if err != nil || string(got) != want {
			t.Errorf("ReadFile(%q) = %q, %v; want %q", p, got, err, want)
		}
	}
}

func TestAssetStorage_ReadStaticFile_staysInsideStaticDir(t *testing.T) {
	t.Parallel()
	const secret = "{{.Secret}} template source"
	files := map[string]string{
		"layouts/home.gohtml":   secret,
		"static/app.css":        "body{}",
		"static/sub/nested.css": "nested{}",
	}

	diskDir := t.TempDir()
	for rel, data := range files {
		target := filepath.Join(diskDir, filepath.FromSlash(rel))
		mustMk(t, filepath.Dir(target))
		mustWrite(t, target, []byte(data))
	}
	// Embedded mode uses a web dir that does not exist on disk, so every successful
	// read (including a traversal) can only come from memory.
	const embeddedRoot = "bungo-static-traversal-web"
	if _, err := os.Stat(embeddedRoot); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("precondition: %q must not exist on disk (stat err: %v)", embeddedRoot, err)
	}
	mem := fstest.MapFS{}
	for rel, data := range files {
		mem[embeddedRoot+"/"+rel] = &fstest.MapFile{Data: []byte(data)}
	}

	escapes := []string{
		"../layouts/home.gohtml",
		`..\layouts\home.gohtml`,
		"./../layouts/home.gohtml",
		"/../layouts/home.gohtml",
		"sub/../../layouts/home.gohtml",
		`sub\..\..\layouts\home.gohtml`,
		"..",
		"../../go.mod",
		"",
	}
	allowed := map[string]string{
		"app.css":        "body{}",
		"/app.css":       "body{}",
		"sub/nested.css": "nested{}",
		`sub\nested.css`: "nested{}",
		"sub/../app.css": "body{}", // a ".." that stays inside static/ is harmless
	}

	stores := []struct {
		name     string
		storage  *AssetStorage
		embedded bool
	}{
		{"disk", newAssetStorage(diskDir, nil), false},
		{"embedded", newAssetStorage(embeddedRoot, mem), true},
	}
	for _, st := range stores {
		t.Run(st.name, func(t *testing.T) {
			s := st.storage
			if (s.memoryFS != nil) != st.embedded {
				t.Fatalf("embedded fs active = %v, want %v", s.memoryFS != nil, st.embedded)
			}
			// The target is readable from the web root, so a traversal would really leak it.
			if b, err := s.ReadFile("layouts/home.gohtml"); err != nil || string(b) != secret {
				t.Fatalf("precondition: layouts/home.gohtml unreadable: %q, %v", b, err)
			}
			for _, p := range escapes {
				if b, err := s.ReadStaticFile(p); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("ReadStaticFile(%q) = %q, %v; want fs.ErrNotExist", p, b, err)
				}
			}
			for p, want := range allowed {
				if b, err := s.ReadStaticFile(p); err != nil || string(b) != want {
					t.Errorf("ReadStaticFile(%q) = %q, %v; want %q", p, b, err, want)
				}
			}
		})
	}
}

func TestAssetStorage_PrepareWebDirForBuild_diskBacked(t *testing.T) {
	t.Parallel()
	dir := newTestWebTree(t)
	got, cleanup, err := newAssetStorage(dir, nil).PrepareWebDirForBuild()
	if err != nil || got != dir {
		t.Fatalf("disk-backed storage should build from its own dir: %q, %v", got, err)
	}
	cleanup()
	// The cleanup for a disk-backed build must never delete the user's real web tree.
	if _, err := os.Stat(filepath.Join(dir, "layouts", "home.gohtml")); err != nil {
		t.Fatalf("cleanup removed the source web dir: %v", err)
	}

	got, cleanup, err = newAssetStorage("", nil).PrepareWebDirForBuild()
	if err != nil || got != "" {
		t.Fatalf("no web dir: %q, %v", got, err)
	}
	cleanup()
}

func TestAssetStorage_PrepareWebDirForBuild_failures(t *testing.T) {
	mem := fstest.MapFS{
		"web/layouts/a.gohtml": &fstest.MapFile{Data: []byte("<html/>")},
		"web/views/v.jsx":      &fstest.MapFile{Data: []byte("export default function V(){return null}")},
	}

	t.Run("temp dir cannot be created", func(t *testing.T) {
		setTempDir(t, filepath.Join(t.TempDir(), "does-not-exist"))
		dir, cleanup, err := newAssetStorage("web", mem).PrepareWebDirForBuild()
		if err == nil || dir != "" {
			t.Fatalf("expected MkdirTemp failure, got dir=%q err=%v", dir, err)
		}
		cleanup() // the returned cleanup must be safe to call on failure
	})

	t.Run("embedded file unreadable", func(t *testing.T) {
		tmp := t.TempDir()
		setTempDir(t, tmp)
		broken := unreadableFS{base: mem, fail: "web/views/v.jsx"}
		dir, cleanup, err := newAssetStorage("web", broken).PrepareWebDirForBuild()
		if !errors.Is(err, errUnreadable) || dir != "" {
			t.Fatalf("expected extraction failure, got dir=%q err=%v", dir, err)
		}
		cleanup()
		entries, err := os.ReadDir(tmp)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("failed extraction left %d temp entries behind", len(entries))
		}
	})
}
