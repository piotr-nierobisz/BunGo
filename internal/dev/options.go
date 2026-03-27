package dev

import (
	"context"
	"io"
	"time"
)

// processHealthTick is how often dev checks if the app process exited.
const processHealthTick = 1 * time.Second

// reloadDebounceWindow coalesces bursty fsnotify events into one restart.
const reloadDebounceWindow = 200 * time.Millisecond

// Options configures the BunGo dev runner.
type Options struct {
	// RunTarget is passed directly to `go run <target>`.
	// If empty, "." is used.
	RunTarget string

	// Stdout / Stderr are used for logging and app process output.
	// If nil, output is discarded.
	Stdout io.Writer
	Stderr io.Writer
}

// Run starts the BunGo dev loop with process restart, file watching, and websocket reload signaling.
// Inputs:
// - ctx: cancellation context used to stop dev mode and trigger graceful shutdown.
// - projectRoot: project root directory used for file watching and `go run` execution.
// - opts: optional dev runner configuration; nil uses defaults and discards process output.
// Outputs:
// - error: non-nil when startup, watcher, websocket server, or runtime loop handling fails.
func Run(ctx context.Context, projectRoot string, opts *Options) error {
	if opts == nil {
		opts = &Options{}
	}
	if opts.RunTarget == "" {
		opts.RunTarget = "."
	}

	runner := newProcessRunner(projectRoot, opts.RunTarget, opts.Stdout, opts.Stderr)

	if err := runner.Restart(); err != nil {
		return err
	}

	watcher, err := newProjectWatcher(projectRoot)
	if err != nil {
		return err
	}
	defer watcher.Close()

	hub := newWSHub()
	wsServer := startDevWebSocketServer(hub)
	wsErrCh := make(chan error, 1)

	go func() {
		wsErrCh <- wsServer.ListenAndServe()
	}()

	// Give the websocket server a short window to fail fast if the port is in use.
	select {
	case err := <-wsErrCh:
		if err != nil && err != httpServerClosed {
			return err
		}
	case <-time.After(150 * time.Millisecond):
	}

	healthTicker := time.NewTicker(processHealthTick)
	defer healthTicker.Stop()
	var reloadTimer *time.Timer
	var reloadReady <-chan time.Time
	defer func() {
		if reloadTimer == nil {
			return
		}
		if !reloadTimer.Stop() {
			select {
			case <-reloadTimer.C:
			default:
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			_ = runner.Stop()
			hub.DisconnectAll()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			wsServer.Shutdown(shutdownCtx) // nolint:errcheck
			cancel()
			return nil

		case err := <-wsErrCh:
			if err != nil && err != httpServerClosed {
				return err
			}

		case err := <-watcher.Errors():
			return err

		case <-watcher.Changes():
			if reloadTimer == nil {
				reloadTimer = time.NewTimer(reloadDebounceWindow)
				reloadReady = reloadTimer.C
				continue
			}
			if !reloadTimer.Stop() {
				select {
				case <-reloadTimer.C:
				default:
				}
			}
			reloadTimer.Reset(reloadDebounceWindow)
			reloadReady = reloadTimer.C

		case <-reloadReady:
			reloadReady = nil
			drainChangeEvents(watcher.Changes())
			logChangeDetected(opts.Stdout)
			hub.DisconnectAll()
			if err := runner.Restart(); err != nil {
				logRestartFailure(opts.Stderr, err)
			}

		case <-healthTicker.C:
			if exited, err := runner.CheckExited(); exited {
				logAppExit(opts.Stderr, err)
			}
		}
	}
}

// drainChangeEvents drains queued watcher change notifications without blocking.
// Inputs:
// - ch: watcher change channel to drain before processing a debounced reload.
// Outputs:
// - none
func drainChangeEvents(ch <-chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
