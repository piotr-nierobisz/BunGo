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

func (w *projectWatcher) Close() error {
	var err error
	w.closeOnce.Do(func() {
		close(w.done)
		err = w.watcher.Close()
	})
	return err
}

func (w *projectWatcher) Changes() <-chan struct{} {
	return w.changes
}

func (w *projectWatcher) Errors() <-chan error {
	return w.errs
}

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

func isIgnoredDir(path, name, projectRoot string) bool {
	if _, skip := ignoredDirs[name]; skip {
		return true
	}
	return strings.HasPrefix(name, ".") && path != projectRoot
}

func isAlreadyWatchedErr(err error) bool {
	// fsnotify does not expose a dedicated sentinel for duplicate watch.
	// Keep this check narrow to avoid swallowing unrelated errors.
	return err != nil && strings.Contains(err.Error(), "already exists")
}

func isRelevantChange(event fsnotify.Event) bool {
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
		return false
	}

	name := filepath.Base(event.Name)
	ext := strings.ToLower(filepath.Ext(name))
	_, ok := watchedExtensions[ext]
	return ok
}

func (w *projectWatcher) pushError(err error) {
	if err == nil {
		return
	}
	select {
	case w.errs <- err:
	default:
	}
}
