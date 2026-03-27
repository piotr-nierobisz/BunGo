package dev

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

var watchedExtensions = map[string]struct{}{
	".go":     {},
	".mod":    {},
	".sum":    {},
	".gohtml": {},
	".html":   {},
	".css":    {},
	".js":     {},
	".jsx":    {},
	".ts":     {},
	".tsx":    {},
}

var ignoredDirs = map[string]struct{}{
	".git":    {},
	".cursor": {},
	".idea":   {},
	".vscode": {},
	"vendor":  {},
	"tmp":     {},
	"dist":    {},
	"build":   {},
	"bin":     {},
}

type projectWatcher struct {
	root    string
	watcher *fsnotify.Watcher

	changes chan struct{}
	errs    chan error
	done    chan struct{}

	closeOnce sync.Once
}

// newProjectWatcher creates a recursive project watcher and starts its event loop.
// Inputs:
// - root: project root directory to watch recursively for relevant source/template changes.
// Outputs:
// - *projectWatcher: initialized watcher with active fsnotify subscriptions.
// - error: non-nil when fsnotify setup or initial directory walk fails.
func newProjectWatcher(root string) (*projectWatcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	pw := &projectWatcher{
		root:    root,
		watcher: fsw,
		changes: make(chan struct{}, 1),
		errs:    make(chan error, 1),
		done:    make(chan struct{}),
	}

	if err := pw.watchDirTree(root); err != nil {
		_ = fsw.Close()
		return nil, err
	}

	go pw.run()
	return pw, nil
}

// Close stops the watcher loop and closes underlying fsnotify resources.
// Inputs:
// - none
// Outputs:
// - error: close error returned by fsnotify watcher shutdown.
func (w *projectWatcher) Close() error {
	var err error
	w.closeOnce.Do(func() {
		close(w.done)
		err = w.watcher.Close()
	})
	return err
}

// Changes returns the debounced change notification channel for reload triggers.
// Inputs:
// - none
// Outputs:
// - <-chan struct{}: channel that receives non-blocking change notifications.
func (w *projectWatcher) Changes() <-chan struct{} {
	return w.changes
}

// Errors returns the watcher error channel.
// Inputs:
// - none
// Outputs:
// - <-chan error: channel carrying watcher/runtime errors.
func (w *projectWatcher) Errors() <-chan error {
	return w.errs
}

// run processes fsnotify events until closed, forwarding changes and errors.
// Inputs:
// - none
// Outputs:
// - none
func (w *projectWatcher) run() {
	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.pushError(err)
		}
	}
}

// handleEvent updates recursive watches and emits reload signals for relevant changes.
// Inputs:
// - event: fsnotify event received from the underlying watcher.
// Outputs:
// - none
func (w *projectWatcher) handleEvent(event fsnotify.Event) {
	if event.Name == "" {
		return
	}

	// Keep recursive watches up to date for newly created directories.
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			if err := w.watchDirTree(event.Name); err != nil {
				w.pushError(err)
			}
		}
	}

	if !isRelevantChange(event) {
		return
	}

	select {
	case w.changes <- struct{}{}:
	default:
	}
}

// watchDirTree recursively registers directory watches under root, skipping ignored paths.
// Inputs:
// - root: directory subtree root to walk and register with fsnotify.
// Outputs:
// - error: non-nil when directory traversal or watch registration fails.
func (w *projectWatcher) watchDirTree(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		name := d.Name()
		if d.IsDir() {
			if isIgnoredDir(path, name, w.root) {
				return filepath.SkipDir
			}
			if err := w.watcher.Add(path); err != nil && !isAlreadyWatchedErr(err) {
				return err
			}
			return nil
		}
		return nil
	})
}

// isIgnoredDir reports whether a directory should be excluded from recursive watching.
// Inputs:
// - path: full directory path encountered during walk.
// - name: base directory name for ignore-list checks.
// - projectRoot: root path that allows leading-dot check exceptions.
// Outputs:
// - bool: true when directory should be skipped from watch registration.
func isIgnoredDir(path, name, projectRoot string) bool {
	if _, skip := ignoredDirs[name]; skip {
		return true
	}
	return strings.HasPrefix(name, ".") && path != projectRoot
}

// isAlreadyWatchedErr reports whether an error indicates duplicate watch registration.
// Inputs:
// - err: error returned by fsnotify watcher Add operation.
// Outputs:
// - bool: true when the error text indicates the path is already watched.
func isAlreadyWatchedErr(err error) bool {
	// fsnotify does not expose a dedicated sentinel for duplicate watch.
	// Keep this check narrow to avoid swallowing unrelated errors.
	return err != nil && strings.Contains(err.Error(), "already exists")
}

// isRelevantChange reports whether an fsnotify event should trigger app reload flow.
// Inputs:
// - event: filesystem event to classify by operation type and file extension.
// Outputs:
// - bool: true when event affects watched extensions and relevant operations.
func isRelevantChange(event fsnotify.Event) bool {
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
		return false
	}

	name := filepath.Base(event.Name)
	ext := strings.ToLower(filepath.Ext(name))
	_, ok := watchedExtensions[ext]
	return ok
}

// pushError sends a watcher error without blocking when the error channel is full.
// Inputs:
// - err: error value to forward to watcher consumers.
// Outputs:
// - none
func (w *projectWatcher) pushError(err error) {
	if err == nil {
		return
	}
	select {
	case w.errs <- err:
	default:
	}
}
