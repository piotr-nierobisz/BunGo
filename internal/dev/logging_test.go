package dev

import (
	"bytes"
	"errors"
	"regexp"
	"testing"
)

func TestLogAppExit(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logAppExit(&buf, errors.New("exit status 2"))
	if got, want := buf.String(), "App process exited: exit status 2\n"; got != want {
		t.Fatalf("with error: %q; want %q", got, want)
	}

	buf.Reset()
	logAppExit(&buf, nil)
	if got, want := buf.String(), "App process exited.\n"; got != want {
		t.Fatalf("clean exit: %q; want %q", got, want)
	}

	logAppExit(nil, errors.New("ignored")) // nil writer must not panic
}

func TestLogRestartFailure(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logRestartFailure(&buf, nil)
	if buf.Len() != 0 {
		t.Fatalf("nil error should log nothing, got %q", buf.String())
	}
	logRestartFailure(&buf, errors.New("go: not found"))
	if got, want := buf.String(), "Restart failed: go: not found\n"; got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
	logRestartFailure(nil, errors.New("ignored"))
}

func TestLogChangeDetected(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logChangeDetected(&buf)
	re := regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}\] change detected, reloading…\n$`)
	if !re.MatchString(buf.String()) {
		t.Fatalf("got %q; want timestamped reload line", buf.String())
	}
	logChangeDetected(nil)
}
