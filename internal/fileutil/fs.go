package fileutil

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// CopyFSTreeToDir recursively copies one fs subtree into a destination directory on disk.
// Inputs:
// - sourceFS: filesystem containing the source tree to copy.
// - sourceRoot: root path inside sourceFS to walk and copy.
// - targetDir: destination directory path on disk.
// Outputs:
// - error: non-nil when walking, reading, creating directories, or writing files fails.
func CopyFSTreeToDir(sourceFS fs.FS, sourceRoot string, targetDir string) error {
	root := NormalizeSlashPath(sourceRoot)
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
