package bungo

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// AssetStorage provides a unified web asset reader with memory-first and disk-fallback lookups.
type AssetStorage struct {
	webDir        string
	diskDir       string
	memoryFS      fs.FS
	memoryWebRoot string
}

// newAssetStorage builds an AssetStorage instance for the provided web directory and embedded fs.
// Inputs:
// - webDir: configured server web directory path where layouts/views/static are expected.
// - memoryFS: optional embedded filesystem registered by `bungo build`.
// Outputs:
// - *AssetStorage: initialized storage that prefers embedded reads and falls back to disk.
func newAssetStorage(webDir string, memoryFS fs.FS) *AssetStorage {
	storage := &AssetStorage{
		webDir:  webDir,
		diskDir: webDir,
	}

	trimmed := strings.TrimSpace(strings.ReplaceAll(webDir, "\\", "/"))
	trimmed = strings.TrimPrefix(trimmed, "./")
	trimmed = strings.TrimPrefix(trimmed, "/")
	memoryRoot := path.Clean(trimmed)
	if memoryFS != nil && memoryRoot != "" {
		if memoryRoot != "." && memoryRoot != ".." && !strings.HasPrefix(memoryRoot, "../") {
			if info, err := fs.Stat(memoryFS, memoryRoot); err == nil && info.IsDir() {
				storage.memoryFS = memoryFS
				storage.memoryWebRoot = memoryRoot
			}
		}
	}

	return storage
}

// Exists reports whether a relative web asset path exists in memory or on disk.
// Inputs:
// - relativePath: slash-style path relative to web root, or empty string for the web root itself.
// Outputs:
// - bool: true when the asset exists in embedded memory or disk storage.
func (s *AssetStorage) Exists(relativePath string) bool {
	if s == nil {
		return false
	}

	if relativePath == "" {
		if s.memoryFS != nil && s.memoryWebRoot != "" {
			if info, err := fs.Stat(s.memoryFS, s.memoryWebRoot); err == nil && info.IsDir() {
				return true
			}
		}
		if s.diskDir != "" {
			if info, err := os.Stat(s.diskDir); err == nil && info.IsDir() {
				return true
			}
		}
		return false
	}

	cleanPath, ok := cleanRelativeAssetPath(relativePath)
	if !ok {
		return false
	}

	if s.memoryFS != nil && s.memoryWebRoot != "" {
		memPath := path.Join(s.memoryWebRoot, cleanPath)
		if _, err := fs.Stat(s.memoryFS, memPath); err == nil {
			return true
		}
	}

	if s.diskDir == "" {
		return false
	}

	diskPath := filepath.Join(s.diskDir, filepath.FromSlash(cleanPath))
	_, err := os.Stat(diskPath)
	return err == nil
}

// ReadFile reads one relative web asset path preferring memory and then falling back to disk.
// Inputs:
// - relativePath: slash-style path relative to web root.
// Outputs:
// - []byte: file contents for the resolved asset path.
// - error: non-nil when the asset is invalid or missing in both memory and disk stores.
func (s *AssetStorage) ReadFile(relativePath string) ([]byte, error) {
	if s == nil {
		return nil, os.ErrNotExist
	}

	cleanPath, ok := cleanRelativeAssetPath(relativePath)
	if !ok {
		return nil, os.ErrNotExist
	}

	if s.memoryFS != nil && s.memoryWebRoot != "" {
		memPath := path.Join(s.memoryWebRoot, cleanPath)
		if data, err := fs.ReadFile(s.memoryFS, memPath); err == nil {
			return data, nil
		}
	}

	if s.diskDir == "" {
		return nil, os.ErrNotExist
	}

	diskPath := filepath.Join(s.diskDir, filepath.FromSlash(cleanPath))
	return os.ReadFile(diskPath)
}

// ReadStaticFile reads a static asset by URL-relative path and enforces path traversal safety.
// Inputs:
// - requestPath: URL-relative path under `/static/`, for example `css/app.css`.
// Outputs:
// - []byte: resolved static file bytes from memory-first, disk-fallback storage.
// - error: non-nil when requestPath is invalid or the static file is not found.
func (s *AssetStorage) ReadStaticFile(requestPath string) ([]byte, error) {
	cleanPath, ok := cleanRelativeAssetPath(path.Join("static", requestPath))
	if !ok {
		return nil, os.ErrNotExist
	}
	return s.ReadFile(cleanPath)
}

// PrepareWebDirForBuild returns a disk directory suitable for esbuild and a cleanup callback.
// Inputs:
// - none
// Outputs:
// - string: filesystem directory containing a materialized `layouts`, `views`, and optional `static`.
// - func(): cleanup callback that removes temporary materialized assets when invoked.
// - error: non-nil when temporary directory creation or embedded asset extraction fails.
func (s *AssetStorage) PrepareWebDirForBuild() (string, func(), error) {
	if s == nil || s.webDir == "" {
		return "", func() {}, nil
	}

	if s.memoryFS == nil || s.memoryWebRoot == "" {
		return s.diskDir, func() {}, nil
	}

	tempDir, err := os.MkdirTemp("", "bungo-web-assets-*")
	if err != nil {
		return "", func() {}, err
	}

	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	targetWebDir := filepath.Join(tempDir, "web")
	if err := os.MkdirAll(targetWebDir, 0755); err != nil {
		cleanup()
		return "", func() {}, err
	}

	if err := CopyFSTreeToDir(s.memoryFS, s.memoryWebRoot, targetWebDir); err != nil {
		cleanup()
		return "", func() {}, err
	}

	return targetWebDir, cleanup, nil
}

// cleanRelativeAssetPath normalizes and validates a web-root relative asset path.
// Inputs:
// - relativePath: asset path to normalize and validate.
// Outputs:
// - string: cleaned slash-separated path safe for lookup inside the web root.
// - bool: true when the path is valid and safe; false when it escapes the root.
func cleanRelativeAssetPath(relativePath string) (string, bool) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(relativePath, "\\", "/"))
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return "", false
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == "/" {
		return "", false
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", false
	}

	return cleaned, true
}

// CopyFSTreeToDir recursively copies one fs subtree into a destination directory on disk.
// Inputs:
// - sourceFS: filesystem containing the source tree to copy.
// - sourceRoot: root path inside sourceFS to walk and copy.
// - targetDir: destination directory path on disk.
// Outputs:
// - error: non-nil when walking, reading, creating directories, or writing files fails.
func CopyFSTreeToDir(sourceFS fs.FS, sourceRoot string, targetDir string) error {
	root := strings.TrimSpace(strings.ReplaceAll(sourceRoot, "\\", "/"))
	if root == "" {
		root = "."
	}
	root = path.Clean(root)

	return fs.WalkDir(sourceFS, root, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relative := strings.TrimPrefix(currentPath, root)
		relative = strings.TrimPrefix(relative, "/")
		targetPath := filepath.Join(targetDir, filepath.FromSlash(relative))

		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		data, readErr := fs.ReadFile(sourceFS, currentPath)
		if readErr != nil {
			return readErr
		}
		if mkErr := os.MkdirAll(filepath.Dir(targetPath), 0755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(targetPath, data, 0644)
	})
}
