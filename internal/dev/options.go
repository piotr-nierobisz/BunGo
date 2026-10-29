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

// Run starts the BunGo dev loop with process restart and file watching.
// Browsers are told to reload by the restart itself: pages hold a live-reload
// websocket on the app's own server (see internal/devreload), which closes when
// the old process exits, and they refresh once the new process is serving.
// Inputs:
// - ctx: cancellation context used to stop dev mode and trigger graceful shutdown.
// - projectRoot: project root directory used for file watching and `go run` execution.
// - opts: optional dev runner configuration; nil uses defaults and discards process output.
// Outputs:
// - error: non-nil when startup, watcher, or runtime loop handling fails;
// the app process is always stopped before Run returns.
func Run(ctx context.Context, projectRoot string, opts *Options) error {
	// Work on a copy so defaults never leak into the caller's Options.
	var cfg Options
	if opts != nil {
		cfg = *opts
	}
	if cfg.RunTarget == "" {
		cfg.RunTarget = "."
	}

	runner := newProcessRunner(projectRoot, cfg.RunTarget, cfg.Stdout, cfg.Stderr)

	if err := runner.Restart(); err != nil {
		return err
	}
	// Every return from here on must stop the app: it runs in its own process
	// group, so it would otherwise outlive bungo and keep its port.
	defer runner.Stop()

	watcher, err := newProjectWatcher(projectRoot)
	if err != nil {
		return err
	}
	defer watcher.Close()

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
			return nil

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
			logChangeDetected(cfg.Stdout)
			if err := runner.Restart(); err != nil {
				logRestartFailure(cfg.Stderr, err)
			}

		case <-healthTicker.C:
			if exited, err := runner.CheckExited(); exited {
				logAppExit(cfg.Stderr, err)
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
