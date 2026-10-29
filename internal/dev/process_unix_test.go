//go:build unix

package dev

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeGoScript stands in for the `go` binary. processRunner always executes
// `go run <target>`, so the run target ($2) selects how the fake app behaves.
// Every invocation appends one line to invocations.log in its working
// directory, which lets tests check the arguments, environment and cwd.
// @SLEEP@ is replaced with an absolute path so the script also works with a
// PATH that contains nothing but the fake toolchain.
const fakeGoScript = `#!/bin/sh
echo "$* dev=$BUNGO_DEV_ENABLED pid=$$" >> invocations.log
case "$2" in
serve|.)
	echo "app stdout"
	echo "signal: interrupt" >&2
	echo "app stderr" >&2
	echo ready
	exec @SLEEP@ 30
	;;
stubborn)
	trap '' INT
	echo ready
	exec @SLEEP@ 30
	;;
exit3)
	echo "boom" >&2
	exit 3
	;;
partial)
	printf 'fatal: config missing' >&2
	exit 1
	;;
interleave)
	printf 'out-1 '
	@SLEEP@ 0.1
	echo err-1 >&2
	@SLEEP@ 0.1
	echo out-2
	@SLEEP@ 0.1
	echo ready
	exec @SLEEP@ 30
	;;
crash-then-serve)
	if [ ! -f crashed.marker ]; then
		: > crashed.marker
		exit 3
	fi
	echo ready
	exec @SLEEP@ 30
	;;
*)
	exit 0
	;;
esac
`

// installFakeGo puts fakeGoScript first on PATH as `go` for the duration of
// the test and returns the directory holding it.
func installFakeGo(t *testing.T) string {
	t.Helper()
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("sleep not available: %v", err)
	}
	bin := t.TempDir()
	script := strings.ReplaceAll(fakeGoScript, "@SLEEP@", sleep)
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bin
}

// processAlive reports whether pid still exists (it is not reaped yet).
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func readInvocations(t *testing.T, root string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "invocations.log"))
	if err != nil {
		t.Fatalf("read invocations.log: %v", err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

// invocationPIDs returns the pid recorded by each fake app invocation.
func invocationPIDs(t *testing.T, root string) []int {
	t.Helper()
	var pids []int
	for _, line := range readInvocations(t, root) {
		if line == "" {
			continue
		}
		var pid int
		i := strings.LastIndex(line, "pid=")
		if i < 0 {
			t.Fatalf("no pid in invocation %q", line)
		}
		if _, err := fmt.Sscanf(line[i:], "pid=%d", &pid); err != nil {
			t.Fatalf("parse pid from %q: %v", line, err)
		}
		pids = append(pids, pid)
	}
	return pids
}

// requireNoSurvivingApp fails if any app launched in root is still running.
// An app may be stopped before its first line runs (and then never logs),
// but a surviving one would log its pid and keep sleeping, so the log is
// watched for a moment.
func requireNoSurvivingApp(t *testing.T, root string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(root, "invocations.log")); err == nil {
			for _, pid := range invocationPIDs(t, root) {
				if processAlive(pid) {
					t.Fatalf("app process %d is still running", pid)
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestSignalProcessGroup_rejectsNonPositivePID(t *testing.T) {
	t.Parallel()
	// pid 0 / -1 would signal our own process group or every process; must be refused.
	for _, pid := range []int{0, -1} {
		err := signalProcessGroup(pid, syscall.Signal(0))
		if err == nil || !strings.Contains(err.Error(), "invalid process id") {
			t.Fatalf("signalProcessGroup(%d) err = %v", pid, err)
		}
	}
}

func TestProcessRunner_runsGoRunInProjectRoot(t *testing.T) {
	installFakeGo(t)
	root := t.TempDir()
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	r := newProcessRunner(root, "serve", stdout, stderr)
	t.Cleanup(r.Stop)

	if err := r.Restart(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "app ready on stdout", func() bool {
		return strings.Contains(stdout.String(), "ready")
	})
	waitFor(t, 5*time.Second, "app stderr", func() bool {
		return strings.Contains(stderr.String(), "app stderr\n")
	})

	// invocations.log is written relative to the cwd, so finding it in root proves cmd.Dir.
	inv := readInvocations(t, root)
	if len(inv) != 1 || !strings.HasPrefix(inv[0], "run serve dev=1 ") {
		t.Fatalf("invocations = %q; want one `go run serve` with BUNGO_DEV_ENABLED=1", inv)
	}
	if !strings.Contains(stdout.String(), "app stdout\n") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stderr.String(), "signal: interrupt") {
		t.Fatalf("stderr should suppress go run's interrupt line, got %q", stderr.String())
	}

	if exited, err := r.CheckExited(); exited || err != nil {
		t.Fatalf("CheckExited on running app = %v, %v", exited, err)
	}

	pid := runnerPID(r)
	if pid == 0 {
		t.Fatal("expected a tracked process")
	}
	r.Stop()
	if processAlive(pid) {
		t.Fatalf("process %d still alive after Stop", pid)
	}
	if runnerPID(r) != 0 {
		t.Fatal("Stop should clear the tracked process")
	}
	r.Stop() // second Stop is a no-op
	if exited, err := r.CheckExited(); exited || err != nil {
		t.Fatalf("CheckExited after Stop = %v, %v", exited, err)
	}
}

func TestProcessRunner_restartReplacesRunningProcess(t *testing.T) {
	installFakeGo(t)
	root := t.TempDir()
	stdout := &syncBuffer{}
	r := newProcessRunner(root, "serve", stdout, io.Discard)
	t.Cleanup(r.Stop)

	if err := r.Restart(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "first instance ready", func() bool {
		return strings.Count(stdout.String(), "ready") == 1
	})
	first := runnerPID(r)

	if err := r.Restart(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "second instance ready", func() bool {
		return strings.Count(stdout.String(), "ready") == 2
	})
	second := runnerPID(r)

	if first == 0 || second == 0 || first == second {
		t.Fatalf("pids first=%d second=%d; want two distinct processes", first, second)
	}
	if processAlive(first) {
		t.Fatalf("old process %d should be stopped before the new one starts", first)
	}
	if !processAlive(second) {
		t.Fatalf("new process %d should be running", second)
	}
	if inv := readInvocations(t, root); len(inv) != 2 {
		t.Fatalf("invocations = %q; want 2", inv)
	}
}

func TestProcessRunner_checkExitedReportsCrashOnce(t *testing.T) {
	installFakeGo(t)
	root := t.TempDir()
	stderr := &syncBuffer{}
	r := newProcessRunner(root, "exit3", nil, stderr)

	if exited, err := r.CheckExited(); exited || err != nil {
		t.Fatalf("CheckExited before start = %v, %v", exited, err)
	}
	if err := r.Restart(); err != nil {
		t.Fatal(err)
	}

	var exitErr error
	waitFor(t, 5*time.Second, "crash to be observed", func() bool {
		exited, err := r.CheckExited()
		exitErr = err
		return exited
	})
	var ee *exec.ExitError
	if !errors.As(exitErr, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("exit error = %v; want exit status 3", exitErr)
	}
	if !strings.Contains(stderr.String(), "boom\n") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if exited, err := r.CheckExited(); exited || err != nil {
		t.Fatalf("exit must be reported only once, got %v, %v", exited, err)
	}
	r.Stop() // nothing left to stop; must not block
}

// TestProcessRunner_flushesFinalStderrLineOnExit is a regression test: a last
// stderr line without a trailing newline used to be dropped when the app exited.
func TestProcessRunner_flushesFinalStderrLineOnExit(t *testing.T) {
	installFakeGo(t)
	root := t.TempDir()
	stderr := &syncBuffer{}
	r := newProcessRunner(root, "partial", nil, stderr)

	if err := r.Restart(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "app exit", func() bool {
		exited, _ := r.CheckExited()
		return exited
	})
	// The exit is reported only after output has been flushed.
	if got, want := stderr.String(), "fatal: config missing\n"; got != want {
		t.Fatalf("stderr = %q; want %q", got, want)
	}
}

func TestProcessRunner_stopAfterUnobservedExit(t *testing.T) {
	installFakeGo(t)
	root := t.TempDir()
	r := newProcessRunner(root, "exit0", nil, nil)

	if err := r.Restart(); err != nil {
		t.Fatal(err)
	}
	pid := runnerPID(r)
	// Wait until the process group is gone (exited and reaped) without calling
	// CheckExited, so Stop has to cope with signalling a vanished group.
	waitFor(t, 5*time.Second, "app to exit", func() bool {
		return errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH)
	})

	start := time.Now()
	r.Stop()
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Stop took %v; an already-exited app must not hit the kill timeout", d)
	}
	if runnerPID(r) != 0 {
		t.Fatal("Stop should clear the tracked process")
	}
}

func TestProcessRunner_stopEscalatesToSIGKILL(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the 3s graceful-stop timeout")
	}
	installFakeGo(t)
	root := t.TempDir()
	stdout := &syncBuffer{}
	r := newProcessRunner(root, "stubborn", stdout, nil)
	t.Cleanup(r.Stop)

	if err := r.Restart(); err != nil {
		t.Fatal(err)
	}
	// "ready" is printed after the SIGINT trap is installed.
	waitFor(t, 5*time.Second, "app ready", func() bool {
		return strings.Contains(stdout.String(), "ready")
	})
	pid := runnerPID(r)

	start := time.Now()
	r.Stop()
	d := time.Since(start)
	if d < stopGracePeriod-100*time.Millisecond || d > 10*time.Second {
		t.Fatalf("Stop took %v; want SIGKILL after the %v grace period", d, stopGracePeriod)
	}
	if processAlive(pid) {
		t.Fatalf("process %d ignoring SIGINT should have been killed", pid)
	}
}

// TestProcessRunner_realGoRun exercises the runner against the real toolchain:
// `go run` must see BUNGO_DEV_ENABLED, and the "signal: interrupt" line that
// go run prints when its child is interrupted must not reach stderr.
func TestProcessRunner_realGoRun(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a program with the go toolchain")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/devapp\n\ngo 1.21\n",
		"main.go": `package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	fmt.Println("dev=" + os.Getenv("BUNGO_DEV_ENABLED"))
	time.Sleep(time.Hour)
}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	r := newProcessRunner(root, ".", stdout, stderr)
	t.Cleanup(r.Stop)
	if err := r.Restart(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Minute, "compiled app output", func() bool {
		return strings.Contains(stdout.String(), "dev=1\n")
	})
	r.Stop()
	// Stop waits for cmd.Wait, which drains stderr, so the output is complete here.
	if strings.Contains(stderr.String(), "signal: interrupt") {
		t.Fatalf("stderr = %q; interrupt line should be suppressed", stderr.String())
	}
}
