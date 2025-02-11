package fs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// EnsureEmptyDirectory ensures the directory given by `path` is empty.
// If the directory does not exist, it will be created with permission bits
// given by `perm`. If the directory exists, it will not simply rm -rf && mkdir -p
// as the calling process may not have permissions to delete the directory. E.g.
// in the case of a pod mount. Rather, it will delete the contents of the directory.
func EnsureEmptyDirectory(path string, perm fs.FileMode) error {
	entries, err := os.ReadDir(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if err := DeleteReadOnlyRecursive(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return os.MkdirAll(path, perm)
}

const (
	ownerWritableFileMode os.FileMode = 0700
	ownerWritableDirMode  os.FileMode = 0700
	ownerReadOnlyFileMode os.FileMode = 0400
	ownerReadOnlyDirMode  os.FileMode = 0500
)

// SetReadOnlyRecursive recursively sets files and directories under the path given by `root` as read-only
func SetReadOnlyRecursive(root string) error {
	return setModeRecursive(root, ownerReadOnlyFileMode, ownerReadOnlyDirMode)
}

// SetWritableRecursive recursively sets files and directories under the path given by `root` as writable
func SetWritableRecursive(root string) error {
	return setModeRecursive(root, ownerWritableFileMode, ownerWritableDirMode)
}

// DeleteReadOnlyRecursive deletes the directory with path given by `root`.
// Prior to deleting the directory, the directory and all descendant files
// and directories are set as writable. If any chmod or deletion error occurs
// it is immediately returned.
func DeleteReadOnlyRecursive(root string) error {
	if err := SetWritableRecursive(root); err != nil {
		return fmt.Errorf("error making directory writable for deletion: %w", err)
	}
	return os.RemoveAll(root)
}

func setModeRecursive(path string, fileMode os.FileMode, dirMode os.FileMode) error {
	return filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}

		switch typ := fi.Mode().Type(); typ {
		case os.ModeSymlink:
			// do not follow symlinks
			// 1. if they resolve to other locations in the root, we'll find them anyway
			// 2. if they resolve to other locations outside the root, we don't want to change their permissions
			return nil
		case os.ModeDir:
			return os.Chmod(path, dirMode)
		case 0: // regular file
			return os.Chmod(path, fileMode)
		default:
			return fmt.Errorf("refusing to change ownership of file %q with type %v", path, typ.String())
		}
	})
}

var (
	ErrNotDirectory = errors.New("not a directory")
)

// GetDirectoryModTime returns the modification time of the directory at dirPath.
// If stat(dirPath) fails, an error is returned with a zero-value time.Time.
// If dirPath is not a directory, an ErrNotDirectory error is returned.
func GetDirectoryModTime(dirPath string) (time.Time, error) {
	dirStat, err := os.Stat(dirPath)
	if err != nil {
		return time.Time{}, err
	}
	if !dirStat.IsDir() {
		return time.Time{}, ErrNotDirectory
	}
	return dirStat.ModTime(), nil
}
