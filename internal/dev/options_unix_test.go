//go:build unix

package dev

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startRun runs Run in the background and returns its result channel.
func startRun(ctx context.Context, root string, opts *Options) <-chan error {
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, root, opts) }()
	return errCh
}

// awaitRun waits for Run to return.
func awaitRun(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

// requireOnlyLatestAppRunning fails if any app instance but the newest is
// still alive. The old instance exiting is what closes browsers' live-reload
// sockets, so a survivor would mean pages never reload.
func requireOnlyLatestAppRunning(t *testing.T, root string) {
	t.Helper()
	pids := invocationPIDs(t, root)
	for _, pid := range pids[:len(pids)-1] {
		if processAlive(pid) {
			t.Fatalf("app %d still running after a reload replaced it", pid)
		}
	}
}

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
}

// mkdirUnreadable creates a directory nobody can list or watch.
func mkdirUnreadable(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0755) })
}

// TestRun_stopsAppWhenProjectCannotBeWatched is a regression test: a watcher
// setup failure used to leave the already-started app running.
func TestRun_stopsAppWhenProjectCannotBeWatched(t *testing.T) {
	skipIfRoot(t)
	installFakeGo(t)
	root := t.TempDir()
	mkdirUnreadable(t, filepath.Join(root, "locked"))

	// The timeout only matters if watching unexpectedly succeeds: Run would
	// then serve until cancelled instead of failing.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := awaitRun(t, startRun(ctx, root, &Options{RunTarget: "serve"}))
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Run err = %v; want permission error from the watcher", err)
	}
	requireNoSurvivingApp(t, root)
}

// TestRun_cleansUpWhenWatcherFailsWhileRunning is a regression test: a watcher
// error during the loop used to return with the app still running.
func TestRun_cleansUpWhenWatcherFailsWhileRunning(t *testing.T) {
	skipIfRoot(t)
	installFakeGo(t)
	root := t.TempDir()
	stdout := &syncBuffer{}

	errCh := startRun(context.Background(), root, &Options{RunTarget: "serve", Stdout: stdout})
	waitFor(t, 10*time.Second, "app ready", func() bool {
		return strings.Contains(stdout.String(), "ready")
	})
	pids := invocationPIDs(t, root)

	// A new directory that cannot be watched surfaces as a watcher error.
	mkdirUnreadable(t, filepath.Join(root, "locked"))
	if err := awaitRun(t, errCh); err == nil {
		t.Fatal("Run should return the watcher error")
	}

	for _, pid := range pids {
		if processAlive(pid) {
			t.Fatalf("app %d still running after Run returned", pid)
		}
	}
}

// TestRun_keepsRunningWhenRestartFails checks that a reload whose restart
// cannot even start the toolchain is reported, and that the next change
// recovers once `go` is available again.
func TestRun_keepsRunningWhenRestartFails(t *testing.T) {
	bin := installFakeGo(t)
	t.Setenv("PATH", bin) // nothing but the fake toolchain, so removing it makes `go` unresolvable
	root := t.TempDir()
	stdout, stderr := &syncBuffer{}, &syncBuffer{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := startRun(ctx, root, &Options{Stdout: stdout, Stderr: stderr}) // default target "."
	running := func(n int) func() bool {
		return func() bool {
			select {
			case err := <-errCh:
				t.Fatalf("Run exited early: %v", err)
			default:
			}
			return strings.Count(stdout.String(), "ready") == n
		}
	}
	waitFor(t, 10*time.Second, "app started", running(1))
	if inv := readInvocations(t, root); len(inv) != 1 || !strings.HasPrefix(inv[0], "run . dev=1 ") {
		t.Fatalf("invocations = %q; want `go run .` for the default target", inv)
	}

	script := filepath.Join(bin, "go")
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(script); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	waitFor(t, 10*time.Second, "restart failure report", func() bool {
		return strings.Contains(stderr.String(), "Restart failed: ") &&
			strings.Contains(stderr.String(), "executable file not found")
	})
	// The old instance was stopped before the failed start.
	for _, pid := range invocationPIDs(t, root) {
		if processAlive(pid) {
			t.Fatalf("app %d should have been stopped for the reload", pid)
		}
	}

	if err := os.WriteFile(script, body, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "main.go"), "package main // fixed\n")
	waitFor(t, 10*time.Second, "app restarted after recovery", running(2))

	cancel()
	if err := awaitRun(t, errCh); err != nil {
		t.Fatalf("Run after cancel = %v", err)
	}
}

// TestRun_reloadLifecycle drives the full dev loop: crash detection, a
// debounced reload on file change that replaces the running app (which is
// what makes browsers reload), and graceful shutdown on context cancellation.
func TestRun_reloadLifecycle(t *testing.T) {
	installFakeGo(t)
	root := t.TempDir()
	stdout, stderr := &syncBuffer{}, &syncBuffer{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := startRun(ctx, root, &Options{RunTarget: "crash-then-serve", Stdout: stdout, Stderr: stderr})

	// The first app instance exits with status 3; the health check reports it.
	waitFor(t, 10*time.Second, "crash report", func() bool {
		return strings.Contains(stderr.String(), "App process exited: exit status 3")
	})

	// First edit: the app is restarted.
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	waitFor(t, 10*time.Second, "first reload", func() bool {
		return strings.Count(stdout.String(), "ready") == 1
	})
	if n := strings.Count(stdout.String(), "change detected, reloading"); n != 1 {
		t.Fatalf("reload log lines = %d; want 1:\n%s", n, stdout.String())
	}

	// A burst of saves is debounced into a single restart that replaces the
	// serving instance.
	for _, name := range []string{"handlers.go", "routes.go", "models.go"} {
		writeFile(t, filepath.Join(root, name), "package main\n")
	}
	waitFor(t, 10*time.Second, "second reload", func() bool {
		return strings.Count(stdout.String(), "ready") == 2
	})
	requireOnlyLatestAppRunning(t, root)
	time.Sleep(2 * reloadDebounceWindow) // give a (wrong) extra restart time to show up
	if n := strings.Count(stdout.String(), "change detected, reloading"); n != 2 {
		t.Fatalf("reload log lines = %d; want 2 (burst must coalesce):\n%s", n, stdout.String())
	}

	cancel()
	if err := awaitRun(t, errCh); err != nil {
		t.Fatalf("Run after cancel = %v; want nil", err)
	}
	inv := readInvocations(t, root)
	if len(inv) != 3 {
		t.Fatalf("invocations = %q; want initial run plus two reloads", inv)
	}
	for _, line := range inv {
		if !strings.HasPrefix(line, "run crash-then-serve dev=1 ") {
			t.Fatalf("unexpected invocation %q", line)
		}
	}
	for _, pid := range invocationPIDs(t, root) {
		if processAlive(pid) {
			t.Fatalf("app %d still running after shutdown", pid)
		}
	}
}
