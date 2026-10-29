//go:build unix

package dev

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"
)

// These tests rely on Unix permission bits to make a directory unwatchable.

func TestNewProjectWatcher_unreadableSubdir(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	root := t.TempDir()
	mkdirUnreadable(t, filepath.Join(root, "locked"))

	_, err := newProjectWatcher(root)
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("err = %v; want permission error", err)
	}
}

func TestProjectWatcher_handleEventReportsUnwatchableDirectory(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	root := t.TempDir()
	pw := newIdleProjectWatcher(t, root)

	locked := filepath.Join(root, "locked")
	mkdirUnreadable(t, locked)

	pw.handleEvent(fsnotify.Event{Name: locked, Op: fsnotify.Create})
	select {
	case err := <-pw.Errors():
		if !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("err = %v; want permission error", err)
		}
	default:
		t.Fatal("expected an error for a directory that cannot be watched")
	}
}
