//go:build unix

package dev

import (
	"context"
	"errors"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// logRecorder collects the log lines the dev model receives.
type logRecorder struct {
	mu    sync.Mutex
	lines []string
}

func (r *logRecorder) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
}

func (r *logRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

// runUIHeadless makes RunUI run without a terminal for the duration of the
// test and records every log line its model receives. Tests using it must
// not run in parallel.
func runUIHeadless(t *testing.T, extra ...tea.ProgramOption) *logRecorder {
	t.Helper()
	rec := &logRecorder{}
	prev := programOptions
	programOptions = append([]tea.ProgramOption{
		tea.WithInput(nil),
		tea.WithOutput(io.Discard),
		tea.WithoutSignalHandler(),
		tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
			if line, ok := msg.(logMsg); ok {
				rec.add(string(line))
			}
			return msg
		}),
	}, extra...)
	t.Cleanup(func() { programOptions = prev })
	return rec
}

// startRunUI runs RunUI in the background with a stop func that counts calls.
func startRunUI(root, entry string) (<-chan error, context.CancelFunc, *atomic.Int32) {
	ctx, cancel := context.WithCancel(context.Background())
	stopCalls := &atomic.Int32{}
	stop := func() {
		stopCalls.Add(1)
		cancel()
	}
	errCh := make(chan error, 1)
	go func() { errCh <- RunUI(ctx, stop, root, entry, "v1.4.2") }()
	return errCh, cancel, stopCalls
}

func awaitRunUI(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("RunUI did not return")
		return nil
	}
}

// TestRunUI_keepsStdoutAndStderrLinesApart is a regression test: stdout and
// stderr shared one line buffer, so a partial stdout line was spliced onto the
// next stderr line ("out-1 err-1") and the rest of it logged alone.
func TestRunUI_keepsStdoutAndStderrLinesApart(t *testing.T) {
	installFakeGo(t)
	rec := runUIHeadless(t)
	root := t.TempDir()

	errCh, cancel, stopCalls := startRunUI(root, "interleave")
	defer cancel()
	waitFor(t, 10*time.Second, "app output", func() bool {
		select {
		case err := <-errCh:
			t.Fatalf("RunUI exited early: %v", err)
		default:
		}
		return len(rec.snapshot()) >= 3
	})

	got := rec.snapshot()
	sort.Strings(got)
	want := []string{"err-1", "out-1 out-2", "ready"}
	if !sameStrings(got, want) {
		t.Fatalf("log lines = %q; want %q", got, want)
	}

	// Cancelling the dev context ends the dev loop, which quits the UI cleanly.
	cancel()
	if err := awaitRunUI(t, errCh); err != nil {
		t.Fatalf("RunUI = %v; want nil after a clean shutdown", err)
	}
	if n := stopCalls.Load(); n != 1 {
		t.Fatalf("stop called %d times; want 1", n)
	}
	requireNoSurvivingApp(t, root)
}

// TestRunUI_stopsDevLoopWhenUIIsKilled checks that a UI that ends abnormally
// cancels the dev loop, waits for it to stop the app, and reports the UI error.
func TestRunUI_stopsDevLoopWhenUIIsKilled(t *testing.T) {
	installFakeGo(t)
	killed, kill := context.WithCancel(context.Background())
	kill()
	runUIHeadless(t, tea.WithContext(killed))
	root := t.TempDir()

	errCh, cancel, stopCalls := startRunUI(root, "serve")
	defer cancel()
	err := awaitRunUI(t, errCh)
	if !errors.Is(err, tea.ErrProgramKilled) {
		t.Fatalf("RunUI = %v; want the UI's ErrProgramKilled", err)
	}
	if n := stopCalls.Load(); n != 1 {
		t.Fatalf("stop called %d times; want 1", n)
	}
	// RunUI waits for the dev loop, so the app it launched must not outlive it.
	requireNoSurvivingApp(t, root)
}
