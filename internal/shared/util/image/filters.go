package image

import (
	"archive/tar"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/archive"
)

// forceOwnershipRWX is a passthrough archive.Filter that sets a tar header's
// Uid and Gid to the current process's Uid and Gid and ensures its permissions
// give the owner full read/write/execute permission. The process Uid and Gid
// are determined when forceOwnershipRWX is called, not when the filter function
// is called.
func forceOwnershipRWX() archive.Filter {
	uid := os.Getuid()
	gid := os.Getgid()
	return func(h *tar.Header) (bool, error) {
		h.Uid = uid
		h.Gid = gid
		h.Mode |= 0700
		h.PAXRecords = nil
		h.Xattrs = nil //nolint:staticcheck
		return true, nil
	}
}

// onlyPath is an archive.Filter that keeps only files and directories that match p, or
// (if p is a directory) are present under p. onlyPath does not remap files to a new location.
// If an error occurs while comparing the desired path prefix with the tar header's name, the
// filter will return false with that error.
func onlyPath(p string) archive.Filter {
	wantPath := path.Clean(strings.TrimPrefix(p, "/"))
	return func(h *tar.Header) (bool, error) {
		headerPath := path.Clean(strings.TrimPrefix(h.Name, "/"))
		relPath, err := filepath.Rel(wantPath, headerPath)
		if err != nil {
			return false, fmt.Errorf("error getting relative path: %w", err)
		}
		if relPath == ".." || strings.HasPrefix(relPath, "../") {
			return false, nil
		}
		return true, nil
	}
}

// allFilters is a composite archive.Filter that executes each filter in the order
// they are given. If any filter returns false or an error, the composite filter will immediately
// return that result to the caller, and no further filters are executed.
func allFilters(filters ...archive.Filter) archive.Filter {
	return func(h *tar.Header) (bool, error) {
		for _, filter := range filters {
			keep, err := filter(h)
			if err != nil {
				return false, err
			}
			if !keep {
				return false, nil
			}
		}
		return true, nil
	}
}
