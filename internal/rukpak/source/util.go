package source

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SetReadOnlyRecursive sets directory with path given by `root` as read-only
func SetReadOnlyRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		fi, err := d.Info()
		if err != nil {
			return err
		}

		if err := func() error {
			switch typ := fi.Mode().Type(); typ {
			case os.ModeSymlink:
				// do not follow symlinks
				// 1. if they resolve to other locations in the root, we'll find them anyway
				// 2. if they resolve to other locations outside the root, we don't want to change their permissions
				return nil
			case os.ModeDir:
				return os.Chmod(path, 0500)
			case 0: // regular file
				return os.Chmod(path, 0400)
			default:
				return fmt.Errorf("refusing to change ownership of file %q with type %v", path, typ.String())
			}
		}(); err != nil {
			return err
		}
		return nil
	})
}

// UnsetReadOnlyRecursive unsets directory with path given by `root` as read-only
func UnsetReadOnlyRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if err := os.Chmod(path, 0700); err != nil {
			return err
		}
		return nil
	})
}

// DeleteReadOnlyRecursive deletes read-only directory with path given by `root`
func DeleteReadOnlyRecursive(root string) error {
	if err := UnsetReadOnlyRecursive(root); err != nil {
		return fmt.Errorf("error making directory writable for deletion: %w", err)
	}
	return os.RemoveAll(root)
}

// IsImageUnpacked checks whether an image has been unpacked in `unpackPath`.
// If true, time of unpack will also be returned. If false unpack time is gibberish (zero/epoch time).
// If `unpackPath` is a file, it will be deleted and false will be returned without an error.
func IsImageUnpacked(unpackPath string) (bool, time.Time, error) {
	unpackStat, err := os.Stat(unpackPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, time.Time{}, nil
		}
		return false, time.Time{}, err
	}
	if !unpackStat.IsDir() {
		return false, time.Time{}, os.Remove(unpackPath)
	}
	return true, unpackStat.ModTime(), nil
}
