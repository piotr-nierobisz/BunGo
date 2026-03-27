package dev

import (
	"fmt"
	"io"
	"time"

	"github.com/piotr-nierobisz/BunGo/internal/theme"
)

// logAppExit logs an app-process exit message with optional exit error details.
// Inputs:
// - stderr: writer used for dev error/status output; nil disables logging.
// - err: process wait error reported when the app exits.
// Outputs:
// - none
func logAppExit(stderr io.Writer, err error) {
	if stderr == nil {
		return
	}
	if err != nil {
		fmt.Fprintf(stderr, theme.EN.Dev.AppExitWithErrFmt, err)
	} else {
		fmt.Fprint(stderr, theme.EN.Dev.AppExitOK)
	}
}

// logRestartFailure logs a restart failure message for the dev runner process.
// Inputs:
// - stderr: writer used for restart error output; nil disables logging.
// - err: restart error to format into the localized failure message.
// Outputs:
// - none
func logRestartFailure(stderr io.Writer, err error) {
	if stderr == nil || err == nil {
		return
	}
	fmt.Fprintf(stderr, theme.EN.Dev.RestartFailedFmt, err)
}

// logChangeDetected logs a timestamped file-change message before app restart.
// Inputs:
// - stdout: writer used for informational dev output; nil disables logging.
// Outputs:
// - none
func logChangeDetected(stdout io.Writer) {
	if stdout == nil {
		return
	}
	fmt.Fprintf(stdout, theme.EN.Dev.ChangeReloadingFmt, time.Now().Format("15:04:05"))
}
