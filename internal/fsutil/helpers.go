package fsutil

import (
	"io/fs"
	"os"
	"path/filepath"
)

// EnsureEmptyDirectory ensures the directory given by `path` is empty.
// If the directory does not exist, it will be created with permission bits
// given by `perm`.
func EnsureEmptyDirectory(path string, perm fs.FileMode) error {
	entries, err := os.ReadDir(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return os.MkdirAll(path, perm)
}
