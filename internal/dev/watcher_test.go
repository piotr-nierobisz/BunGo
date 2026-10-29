package dev

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestIsRelevantChange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		op   fsnotify.Op
		want bool
	}{
		{"main.go", fsnotify.Write, true},
		{"go.mod", fsnotify.Write, true},
		{"go.sum", fsnotify.Create, true},
		{"web/layouts/base.gohtml", fsnotify.Write, true},
		{"web/views/Home.JSX", fsnotify.Write, true}, // extension match is case-insensitive
		{"web/static/app.css", fsnotify.Remove, true},
		{"web/views/page.tsx", fsnotify.Rename, true},
		{"web/static/index.html", fsnotify.Create, true},
		{"lib/util.ts", fsnotify.Write, true},
		{"lib/util.js", fsnotify.Write, true},
		{"main.go", fsnotify.Chmod, false}, // metadata-only changes do not reload
		{"main.go", fsnotify.Write | fsnotify.Chmod, true},
		{"README.md", fsnotify.Write, false},
		{"notes.txt", fsnotify.Create, false},
		{"web/views", fsnotify.Create, false}, // directory names carry no extension
		{"main.go.swp", fsnotify.Write, false},
	}
	for _, tt := range tests {
		if got := isRelevantChange(fsnotify.Event{Name: tt.name, Op: tt.op}); got != tt.want {
			t.Errorf("isRelevantChange(%q, %v) = %v; want %v", tt.name, tt.op, got, tt.want)
		}
	}
}

func TestIsIgnoredDir(t *testing.T) {
	t.Parallel()
	const root = "/work/app"
	tests := []struct {
		path string
		want bool
	}{
		{"/work/app/web", false},
		{"/work/app/web/views", false},
		{"/work/app/.git", true},
		{"/work/app/.vscode", true},
		{"/work/app/.cache", true}, // any dot-dir below the root
		{"/work/app/vendor", true},
		{"/work/app/pkg/vendor", true}, // ignore list applies at any depth
		{"/work/app/dist", true},
		{"/work/app/build", true},
		{"/work/app/bin", true},
		{"/work/app/tmp", true},
	}
	for _, tt := range tests {
		if got := isIgnoredDir(tt.path, filepath.Base(tt.path), root); got != tt.want {
			t.Errorf("isIgnoredDir(%q) = %v; want %v", tt.path, got, tt.want)
		}
	}

	// The project root itself is never ignored, whatever it is called
	// (regression: roots named like ignored dirs used to be skipped).
	for _, name := range []string{".app", "build", "bin", "dist", "tmp", "vendor"} {
		projectRoot := "/work/" + name
		if isIgnoredDir(projectRoot, name, projectRoot) {
			t.Errorf("project root %q must not be ignored", projectRoot)
		}
		// ...but the same name below the root still is.
		if nested := projectRoot + "/" + name; !isIgnoredDir(nested, name, projectRoot) {
			t.Errorf("nested %q should still be ignored", nested)
		}
	}
}

func TestIsAlreadyWatchedErr(t *testing.T) {
	t.Parallel()
	if isAlreadyWatchedErr(nil) {
		t.Error("nil error")
	}
	if !isAlreadyWatchedErr(errors.New("inotify: watch already exists")) {
		t.Error("duplicate-watch error should be recognised")
	}
	if isAlreadyWatchedErr(fs.ErrPermission) {
		t.Error("unrelated errors must not be swallowed")
	}
}

func mkdirs(t *testing.T, root string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0755); err != nil {
			t.Fatal(err)
		}
	}
}

func sortedWatchList(w *fsnotify.Watcher) []string {
	list := w.WatchList()
	for i := range list {
		list[i] = filepath.Clean(list[i])
	}
	sort.Strings(list)
	return list
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNewProjectWatcher_watchesTreeSkippingIgnoredDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mkdirs(t, root,
		"web/views", "web/layouts", "pkg/inner",
		".git/objects", ".idea", ".hidden/deep", "vendor/example.com/mod",
		"dist", "build", "bin", "tmp", "pkg/inner/vendor/x",
	)

	pw, err := newProjectWatcher(root)
	if err != nil {
		t.Fatal(err)
	}
	defer pw.Close()

	want := []string{root}
	for _, rel := range []string{"web", "web/views", "web/layouts", "pkg", "pkg/inner"} {
		want = append(want, filepath.Join(root, filepath.FromSlash(rel)))
	}
	sort.Strings(want)
	if got := sortedWatchList(pw.watcher); !sameStrings(got, want) {
		t.Fatalf("watched dirs:\n got %q\nwant %q", got, want)
	}
}

// TestNewProjectWatcher_rootNamedLikeIgnoredDir is a regression test: a project
// living in a directory called build/bin/dist/tmp/vendor was never watched.
func TestNewProjectWatcher_rootNamedLikeIgnoredDir(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"build", "bin", "dist", "tmp", "vendor"} {
		root := filepath.Join(t.TempDir(), name)
		mkdirs(t, root, "web/views", name) // nested same-named dir stays ignored

		pw, err := newProjectWatcher(root)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{root, filepath.Join(root, "web"), filepath.Join(root, "web", "views")}
		sort.Strings(want)
		got := sortedWatchList(pw.watcher)
		_ = pw.Close()
		if !sameStrings(got, want) {
			t.Fatalf("root %q watched dirs:\n got %q\nwant %q", name, got, want)
		}
	}
}

func TestNewProjectWatcher_missingRoot(t *testing.T) {
	t.Parallel()
	_, err := newProjectWatcher(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v; want not-exist", err)
	}
}

// newIdleProjectWatcher builds a projectWatcher around a real fsnotify watcher
// without starting its event loop, so handleEvent/run can be driven directly.
func newIdleProjectWatcher(t *testing.T, root string) *projectWatcher {
	t.Helper()
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	pw := &projectWatcher{
		root:    root,
		watcher: fsw,
		changes: make(chan struct{}, 1),
		errs:    make(chan error, 1),
		done:    make(chan struct{}),
	}
	t.Cleanup(func() { _ = pw.Close() })
	return pw
}

func pendingChanges(pw *projectWatcher) int {
	n := 0
	for {
		select {
		case <-pw.changes:
			n++
		default:
			return n
		}
	}
}

func TestProjectWatcher_handleEventSignalsRelevantChanges(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pw := newIdleProjectWatcher(t, root)

	pw.handleEvent(fsnotify.Event{Name: "", Op: fsnotify.Write})
	pw.handleEvent(fsnotify.Event{Name: filepath.Join(root, "notes.txt"), Op: fsnotify.Write})
	pw.handleEvent(fsnotify.Event{Name: filepath.Join(root, "main.go"), Op: fsnotify.Chmod})
	if n := pendingChanges(pw); n != 0 {
		t.Fatalf("irrelevant events produced %d change signals", n)
	}

	// A burst of relevant events coalesces into a single pending signal and never blocks.
	for i := 0; i < 5; i++ {
		pw.handleEvent(fsnotify.Event{Name: filepath.Join(root, "main.go"), Op: fsnotify.Write})
	}
	if n := pendingChanges(pw); n != 1 {
		t.Fatalf("burst produced %d pending signals; want 1", n)
	}
}

func TestProjectWatcher_handleEventWatchesNewDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pw := newIdleProjectWatcher(t, root)
	if _, err := pw.watchDirTree(root); err != nil {
		t.Fatal(err)
	}

	mkdirs(t, root, "feature/sub", "vendor/dep", ".cache")
	for _, rel := range []string{"feature", "vendor", ".cache"} {
		pw.handleEvent(fsnotify.Event{Name: filepath.Join(root, rel), Op: fsnotify.Create})
	}
	// Create events for plain files must not be treated as directories.
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	pw.handleEvent(fsnotify.Event{Name: file, Op: fsnotify.Create})

	want := []string{root, filepath.Join(root, "feature"), filepath.Join(root, "feature", "sub")}
	sort.Strings(want)
	if got := sortedWatchList(pw.watcher); !sameStrings(got, want) {
		t.Fatalf("watched dirs:\n got %q\nwant %q", got, want)
	}
	if n := pendingChanges(pw); n != 1 {
		t.Fatalf("pending changes = %d; only main.go should signal", n)
	}
	select {
	case err := <-pw.errs:
		t.Fatalf("unexpected watcher error: %v", err)
	default:
	}
}

// TestProjectWatcher_newDirectoryWithSourcesSignalsChange is a regression test:
// a directory created or moved in with source files already inside produced
// no reload, because only the directory itself generated an event.
func TestProjectWatcher_newDirectoryWithSourcesSignalsChange(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pw := newIdleProjectWatcher(t, root)

	tests := []struct {
		dir   string
		files []string
		want  int
	}{
		{"feature", []string{"feature/handler.go"}, 1},
		{"deep", []string{"deep/a/b/view.jsx"}, 1},
		{"docs", []string{"docs/README.md", "docs/img/logo.png"}, 0},
		{"empty", nil, 0},
		{"thirdparty", []string{"thirdparty/vendor/lib/lib.go", "thirdparty/.cache/x.go"}, 0}, // only in ignored subdirs
		{"vendor", []string{"vendor/dep/dep.go"}, 0},                                          // ignored itself
	}
	for _, tt := range tests {
		mkdirs(t, root, tt.dir)
		for _, f := range tt.files {
			path := filepath.Join(root, filepath.FromSlash(f))
			mkdirs(t, root, filepath.ToSlash(filepath.Dir(filepath.FromSlash(f))))
			writeFile(t, path, "x\n")
		}
		pw.handleEvent(fsnotify.Event{Name: filepath.Join(root, tt.dir), Op: fsnotify.Create})
		if n := pendingChanges(pw); n != tt.want {
			t.Errorf("new dir %q: %d change signals; want %d", tt.dir, n, tt.want)
		}
	}
}

func TestProjectWatcher_pushErrorNeverBlocks(t *testing.T) {
	t.Parallel()
	pw := &projectWatcher{errs: make(chan error, 1)}
	pw.pushError(nil)
	if len(pw.errs) != 0 {
		t.Fatal("nil error must not be queued")
	}
	first, second := errors.New("first"), errors.New("second")
	pw.pushError(first)
	pw.pushError(second) // channel full: dropped instead of blocking
	if got := <-pw.errs; got != first {
		t.Fatalf("got %v; want the first queued error", got)
	}
}

func TestProjectWatcher_runForwardsBackendEventsAndErrors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pw := newIdleProjectWatcher(t, root)
	events, errs := make(chan fsnotify.Event), make(chan error)
	pw.watcher.Events, pw.watcher.Errors = events, errs

	exited := make(chan struct{})
	go func() {
		pw.run()
		close(exited)
	}()

	events <- fsnotify.Event{Name: filepath.Join(root, "main.go"), Op: fsnotify.Write}
	select {
	case <-pw.Changes():
	case <-time.After(5 * time.Second):
		t.Fatal("relevant event was not forwarded as a change")
	}

	backendErr := errors.New("kernel event queue overflow")
	errs <- backendErr
	select {
	case err := <-pw.Errors():
		if err != backendErr {
			t.Fatalf("got %v; want %v", err, backendErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("backend error was not forwarded")
	}

	if err := pw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("second Close should be a no-op, got %v", err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop after Close")
	}
}

func TestProjectWatcher_runStopsWhenBackendChannelsClose(t *testing.T) {
	t.Parallel()
	for _, closeEvents := range []bool{true, false} {
		pw := newIdleProjectWatcher(t, t.TempDir())
		events, errs := make(chan fsnotify.Event), make(chan error)
		pw.watcher.Events, pw.watcher.Errors = events, errs

		exited := make(chan struct{})
		go func() {
			pw.run()
			close(exited)
		}()
		if closeEvents {
			close(events)
		} else {
			close(errs)
		}
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Fatalf("run kept going after backend closed (events closed: %v)", closeEvents)
		}
	}
}

// settle drains change signals until the watcher has been quiet for a while,
// so the next assertion only sees events caused by the next write.
func settle(pw *projectWatcher) {
	for {
		select {
		case <-pw.Changes():
		case <-time.After(150 * time.Millisecond):
			return
		}
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestProjectWatcher_detectsFileChangesEndToEnd uses the real fsnotify backend.
func TestProjectWatcher_detectsFileChangesEndToEnd(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mkdirs(t, root, "web/views", "vendor/dep", ".git")
	pw, err := newProjectWatcher(root)
	if err != nil {
		t.Fatal(err)
	}
	defer pw.Close()

	expectChange := func(what string) {
		t.Helper()
		select {
		case <-pw.Changes():
		case err := <-pw.Errors():
			t.Fatalf("watcher error: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatalf("no change detected for %s", what)
		}
	}

	// A view in a nested directory that existed at startup.
	writeFile(t, filepath.Join(root, "web", "views", "Home.jsx"), "export default function Home(){return null}\n")
	expectChange("a nested view file")
	settle(pw)

	// Ignored directories and irrelevant files stay silent.
	writeFile(t, filepath.Join(root, "vendor", "dep", "dep.go"), "package dep\n")
	writeFile(t, filepath.Join(root, ".git", "hook.go"), "package hook\n")
	writeFile(t, filepath.Join(root, "notes.txt"), "todo\n")
	select {
	case <-pw.Changes():
		t.Fatal("change signalled for ignored directories or irrelevant files")
	case err := <-pw.Errors():
		t.Fatalf("watcher error: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	// A package moved in from elsewhere already contains its sources, so the
	// move itself must trigger a reload (regression: it used to be ignored).
	staging := filepath.Join(t.TempDir(), "moved")
	mkdirs(t, staging, "sub")
	writeFile(t, filepath.Join(staging, "sub", "moved.go"), "package sub\n")
	if err := os.Rename(staging, filepath.Join(root, "moved")); err != nil {
		t.Fatal(err)
	}
	expectChange("a directory with sources moved into the project")
	settle(pw)

	// Edits in a newly created package directory are picked up once the
	// watcher has registered it; keep touching the file until that happens.
	newPkg := filepath.Join(root, "feature")
	if err := os.Mkdir(newPkg, 0755); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		writeFile(t, filepath.Join(newPkg, "feature.go"), "package feature\n")
		select {
		case <-pw.Changes():
			return
		case err := <-pw.Errors():
			t.Fatalf("watcher error: %v", err)
		case <-time.After(100 * time.Millisecond):
			if time.Now().After(deadline) {
				t.Fatal("no change detected for a file in a new directory")
			}
		}
	}
}
