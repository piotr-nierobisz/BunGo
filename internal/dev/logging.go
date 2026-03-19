package dev

import (
	"fmt"
	"io"
	"time"

	"github.com/piotr-nierobisz/BunGo/internal/theme"
)

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

func logRestartFailure(stderr io.Writer, err error) {
	if stderr == nil || err == nil {
		return
	}
	fmt.Fprintf(stderr, theme.EN.Dev.RestartFailedFmt, err)
}

func logChangeDetected(stdout io.Writer) {
	if stdout == nil {
		return
	}
	fmt.Fprintf(stdout, theme.EN.Dev.ChangeReloadingFmt, time.Now().Format("15:04:05"))
}
