package dev

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Tests that need /bin/sh or Unix signals live in process_unix_test.go.

// syncBuffer is a bytes.Buffer safe for concurrent writes from exec's copy goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor polls cond until it returns true or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", timeout, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// runnerPID returns the pid of the runner's tracked process, or 0 if none.
func runnerPID(r *processRunner) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == nil || r.cmd.Process == nil {
		return 0
	}
	return r.cmd.Process.Pid
}

func TestNewLineSuppressingWriter_disabled(t *testing.T) {
	t.Parallel()
	if got := newLineSuppressingWriter(nil, "signal: interrupt"); got != nil {
		t.Fatalf("nil destination should stay nil, got %#v", got)
	}
	var buf bytes.Buffer
	if got := newLineSuppressingWriter(&buf, ""); got != io.Writer(&buf) {
		t.Fatalf("empty suppress line should return dst unchanged, got %#v", got)
	}
}

func TestLineSuppressingWriter_dropsOnlyExactLines(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := newLineSuppressingWriter(&buf, "signal: interrupt")

	// Chunks deliberately split lines (including the suppressed one) mid-way.
	chunks := []string{
		"first\nsig",
		"nal: interrupt\nsignal: interrupted\n",
		"  signal: interrupt\nlast",
	}
	for _, c := range chunks {
		n, err := w.Write([]byte(c))
		if err != nil || n != len(c) {
			t.Fatalf("Write(%q) = %d, %v; want %d, nil", c, n, err, len(c))
		}
	}
	want := "first\nsignal: interrupted\n  signal: interrupt\n"
	if got := buf.String(); got != want {
		t.Fatalf("output = %q; want %q (incomplete trailing line must stay buffered)", got, want)
	}

	if _, err := w.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != want+"last\n" {
		t.Fatalf("after newline output = %q", got)
	}
}

var errSinkFull = errors.New("sink full")

// failingWriter fails on the failAt-th call to Write (1-based).
type failingWriter struct {
	calls  int
	failAt int
}

func (f *failingWriter) Write(p []byte) (int, error) {
	f.calls++
	if f.calls == f.failAt {
		return 0, errSinkFull
	}
	return len(p), nil
}

func TestLineSuppressingWriter_propagatesWriteErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		failAt int
	}{
		{"line body", 1},
		{"line terminator", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newLineSuppressingWriter(&failingWriter{failAt: tc.failAt}, "x")
			n, err := w.Write([]byte("hello\n"))
			if !errors.Is(err, errSinkFull) {
				t.Fatalf("err = %v; want %v", err, errSinkFull)
			}
			if n != len("hello\n") {
				t.Fatalf("n = %d; input must be reported as consumed", n)
			}
		})
	}
}

// TestLineSuppressingWriter_flushCompletesFinalLine is a regression test: a
// final line without a trailing newline used to be dropped.
func TestLineSuppressingWriter_flushCompletesFinalLine(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := newLineSuppressingWriter(&buf, "signal: interrupt").(*lineSuppressingWriter)

	if _, err := w.Write([]byte("compiling\nfatal: no newline")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), "compiling\nfatal: no newline\n"; got != want {
		t.Fatalf("output = %q; want %q", got, want)
	}
	if err := w.Flush(); err != nil || buf.String() != "compiling\nfatal: no newline\n" {
		t.Fatalf("second Flush must be a no-op: %q, %v", buf.String(), err)
	}

	// A trailing suppressed line stays suppressed when flushed.
	buf.Reset()
	if _, err := w.Write([]byte("signal: interrupt")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil || buf.Len() != 0 {
		t.Fatalf("flushed suppressed line: %q, %v", buf.String(), err)
	}

	// Write errors from the destination are reported.
	fw := newLineSuppressingWriter(&failingWriter{failAt: 1}, "x").(*lineSuppressingWriter)
	if _, err := fw.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	if err := fw.Flush(); !errors.Is(err, errSinkFull) {
		t.Fatalf("Flush err = %v; want %v", err, errSinkFull)
	}
}

func TestProcessRunner_restartFailsForMissingProjectRoot(t *testing.T) {
	t.Parallel()
	r := newProcessRunner(filepath.Join(t.TempDir(), "missing"), ".", nil, nil)
	err := r.Restart()
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Restart err = %v; want not-exist error", err)
	}
	if runnerPID(r) != 0 {
		t.Fatal("failed start must not leave a tracked process")
	}
	if exited, err := r.CheckExited(); exited || err != nil {
		t.Fatalf("CheckExited = %v, %v", exited, err)
	}
}
