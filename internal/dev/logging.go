package dev

import (
	"fmt"
	"io"
	"time"
)

func logAppExit(stderr io.Writer, err error) {
	if stderr == nil {
		return
	}
	if err != nil {
		fmt.Fprintf(stderr, "App process exited: %v\n", err)
	} else {
		fmt.Fprintln(stderr, "App process exited.")
	}
}

func logRestartFailure(stderr io.Writer, err error) {
	if stderr == nil || err == nil {
		return
	}
	fmt.Fprintf(stderr, "Restart failed: %v\n", err)
}

func logChangeDetected(stdout io.Writer) {
	if stdout == nil {
		return
	}
	fmt.Fprintf(stdout, "[%s] change detected, reloading…\n", time.Now().Format("15:04:05"))
}
