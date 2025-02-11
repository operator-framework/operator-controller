package image

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/archive"
	"github.com/containers/image/v5/pkg/blobinfocache/none"
	"github.com/containers/image/v5/pkg/compression"
	"github.com/containers/image/v5/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	fsutil "github.com/operator-framework/operator-controller/internal/shared/util/fs"
)

// ForceOwnershipRWX is a passthrough archive.Filter that sets a tar header's
// Uid and Gid to the current process's Uid and Gid and ensures its permissions
// give the owner full read/write/execute permission. The process Uid and Gid
// are determined when ForceOwnershipRWX is called, not when the filter function
// is called.
func ForceOwnershipRWX() archive.Filter {
	uid := os.Getuid()
	gid := os.Getgid()
	return func(h *tar.Header) (bool, error) {
		h.Uid = uid
		h.Gid = gid
		h.Mode |= 0700
		return true, nil
	}
}

// OnlyPath is an archive.Filter that keeps only files and directories that match p, or
// (if p is a directory) are present under p. OnlyPath does not remap files to a new location.
// If an error occurs while comparing the desired path prefix with the tar header's name, the
// filter will return false with that error.
func OnlyPath(p string) archive.Filter {
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

// AllFilters is a composite archive.Filter that executes each filter in the order
// they are given. If any filter returns false or an error, the composite filter will immediately
// return that result to the caller, and no further filters are executed.
func AllFilters(filters ...archive.Filter) archive.Filter {
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

// ApplyLayersToDisk writes the layers from img and imgSrc to disk using the provided filter.
// The destination directory will be created, if necessary. If dest is already present, its
// contents will be deleted. If img and imgSrc do not represent the same image, an error will
// be returned due to a mismatch in the expected layers. Once complete, the dest and its contents
// are marked as read-only to provide a safeguard against unintended changes.
func ApplyLayersToDisk(ctx context.Context, dest string, img types.Image, imgSrc types.ImageSource, filter archive.Filter) error {
	var applyOpts []archive.ApplyOpt
	if filter != nil {
		applyOpts = append(applyOpts, archive.WithFilter(filter))
	}

	if err := fsutil.EnsureEmptyDirectory(dest, 0700); err != nil {
		return fmt.Errorf("error ensuring empty unpack directory: %w", err)
	}
	l := log.FromContext(ctx)
	l.Info("unpacking image", "path", dest)
	for i, layerInfo := range img.LayerInfos() {
		if err := func() error {
			layerReader, _, err := imgSrc.GetBlob(ctx, layerInfo, none.NoCache)
			if err != nil {
				return fmt.Errorf("error getting blob for layer[%d]: %w", i, err)
			}
			defer layerReader.Close()

			decompressed, _, err := compression.AutoDecompress(layerReader)
			if err != nil {
				return fmt.Errorf("auto-decompress failed: %w", err)
			}
			defer decompressed.Close()

			if _, err := archive.Apply(ctx, dest, decompressed, applyOpts...); err != nil {
				return fmt.Errorf("error applying layer[%d]: %w", i, err)
			}
			l.Info("applied layer", "layer", i)
			return nil
		}(); err != nil {
			return errors.Join(err, fsutil.DeleteReadOnlyRecursive(dest))
		}
	}
	if err := fsutil.SetReadOnlyRecursive(dest); err != nil {
		return fmt.Errorf("error making unpack directory read-only: %w", err)
	}
	return nil
}
