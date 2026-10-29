package dev

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

// Run tests that launch a fake app need /bin/sh and live in options_unix_test.go.

func TestDrainChangeEvents(t *testing.T) {
	t.Parallel()
	ch := make(chan struct{}, 3)
	ch <- struct{}{}
	ch <- struct{}{}
	drainChangeEvents(ch)
	if len(ch) != 0 {
		t.Fatalf("%d events left after drain", len(ch))
	}
	drainChangeEvents(ch) // empty channel: returns immediately
}

func TestRun_failsWhenAppCannotStart(t *testing.T) {
	t.Parallel()
	err := Run(context.Background(), filepath.Join(t.TempDir(), "missing"), nil)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Run err = %v; want not-exist error from starting the app", err)
	}
}

// TestRun_doesNotMutateOptions is a regression test: Run used to write its
// default run target into the caller's Options.
func TestRun_doesNotMutateOptions(t *testing.T) {
	t.Parallel()
	opts := &Options{}
	if err := Run(context.Background(), filepath.Join(t.TempDir(), "missing"), opts); err == nil {
		t.Fatal("expected start failure")
	}
	if opts.RunTarget != "" {
		t.Fatalf("caller's RunTarget changed to %q", opts.RunTarget)
	}
}
