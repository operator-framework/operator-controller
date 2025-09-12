package image

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/containerd/containerd/archive"
	"github.com/containers/image/v5/docker/reference"
	"github.com/google/renameio/v2"
	"github.com/opencontainers/go-digest"
	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/registry"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
	errorutil "github.com/operator-framework/operator-controller/internal/shared/util/error"
	fsutil "github.com/operator-framework/operator-controller/internal/shared/util/fs"
)

type LayerData struct {
	MediaType string
	Reader    io.Reader
	Index     int
	Err       error
}

type Cache interface {
	Fetch(context.Context, string, reference.Canonical) (fs.FS, time.Time, error)
	Store(context.Context, string, reference.Named, reference.Canonical, ocispecv1.Image, iter.Seq[LayerData]) (fs.FS, time.Time, error)
	Delete(context.Context, string) error
	GarbageCollect(context.Context, string, reference.Canonical) error
}

const ConfigDirLabel = "operators.operatorframework.io.index.configs.v1"

func CatalogCache(basePath string) Cache {
	return &diskCache{
		basePath:   basePath,
		filterFunc: filterForCatalogImage(),
	}
}

func filterForCatalogImage() func(ctx context.Context, srcRef reference.Named, image ocispecv1.Image) (archive.Filter, error) {
	return func(ctx context.Context, srcRef reference.Named, image ocispecv1.Image) (archive.Filter, error) {
		_, specIsCanonical := srcRef.(reference.Canonical)

		dirToUnpack, ok := image.Config.Labels[ConfigDirLabel]
		if !ok {
			// If the spec is a tagged keep, retries could end up resolving a new digest, where the label
			// might show up. If the spec is canonical, no amount of retries will make the label appear.
			// Therefore, we treat the error as terminal if the reference from the spec is canonical.
			return nil, errorutil.WrapTerminal(fmt.Errorf("catalog image is missing the required label %q", ConfigDirLabel), specIsCanonical)
		}

		return allFilters(
			onlyPath(dirToUnpack),
			forceOwnershipRWX(),
		), nil
	}
}

func BundleCache(basePath string) Cache {
	return &diskCache{
		basePath:   basePath,
		filterFunc: filterForBundleImage(),
	}
}

func filterForBundleImage() func(ctx context.Context, srcRef reference.Named, image ocispecv1.Image) (archive.Filter, error) {
	return func(ctx context.Context, srcRef reference.Named, image ocispecv1.Image) (archive.Filter, error) {
		return forceOwnershipRWX(), nil
	}
}

type diskCache struct {
	basePath   string
	filterFunc func(context.Context, reference.Named, ocispecv1.Image) (archive.Filter, error)
}

func (a *diskCache) Fetch(ctx context.Context, ownerID string, canonicalRef reference.Canonical) (fs.FS, time.Time, error) {
	l := log.FromContext(ctx)
	unpackPath := a.unpackPath(ownerID, canonicalRef.Digest())
	modTime, err := fsutil.GetDirectoryModTime(unpackPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, time.Time{}, nil
	case errors.Is(err, fsutil.ErrNotDirectory):
		l.Info("unpack path is not a directory; attempting to delete", "path", unpackPath)
		return nil, time.Time{}, fsutil.DeleteReadOnlyRecursive(unpackPath)
	case err != nil:
		return nil, time.Time{}, fmt.Errorf("error checking image content already unpacked: %w", err)
	}
	l.Info("image already unpacked")
	return os.DirFS(a.unpackPath(ownerID, canonicalRef.Digest())), modTime, nil
}

func (a *diskCache) ownerIDPath(ownerID string) string {
	return filepath.Join(a.basePath, ownerID)
}

func (a *diskCache) unpackPath(ownerID string, digest digest.Digest) string {
	return filepath.Join(a.ownerIDPath(ownerID), digest.String())
}

type LayerUnpacker interface {
	Unpack(_ context.Context, path string, layer LayerData, opts ...archive.ApplyOpt) error
}

type defaultLayerUnpacker struct{}

type chartLayerUnpacker struct{}

var _ LayerUnpacker = &defaultLayerUnpacker{}
var _ LayerUnpacker = &chartLayerUnpacker{}

func imageLayerUnpacker(layer LayerData) LayerUnpacker {
	if features.OperatorControllerFeatureGate.Enabled(features.HelmChartSupport) || testing.Testing() {
		if layer.MediaType == registry.ChartLayerMediaType {
			return &chartLayerUnpacker{}
		}
	}
	return &defaultLayerUnpacker{}
}

func (u *chartLayerUnpacker) Unpack(_ context.Context, path string, layer LayerData, _ ...archive.ApplyOpt) error {
	if err := storeChartLayer(path, layer); err != nil {
		return fmt.Errorf("error applying chart layer[%d]: %w", layer.Index, err)
	}
	return nil
}

func (u *defaultLayerUnpacker) Unpack(ctx context.Context, path string, layer LayerData, opts ...archive.ApplyOpt) error {
	if _, err := archive.Apply(ctx, path, layer.Reader, opts...); err != nil {
		return fmt.Errorf("error applying layer[%d]: %w", layer.Index, err)
	}
	return nil
}

func (a *diskCache) Store(ctx context.Context, ownerID string, srcRef reference.Named, canonicalRef reference.Canonical, imgCfg ocispecv1.Image, layers iter.Seq[LayerData]) (fs.FS, time.Time, error) {
	var applyOpts []archive.ApplyOpt
	if a.filterFunc != nil {
		filter, err := a.filterFunc(ctx, srcRef, imgCfg)
		if err != nil {
			return nil, time.Time{}, err
		}
		applyOpts = append(applyOpts, archive.WithFilter(filter))
	}

	dest := a.unpackPath(ownerID, canonicalRef.Digest())
	if err := fsutil.EnsureEmptyDirectory(dest, 0700); err != nil {
		return nil, time.Time{}, fmt.Errorf("error ensuring empty unpack directory: %w", err)
	}

	if err := func() error {
		l := log.FromContext(ctx)
		l.Info("unpacking image", "path", dest)
		for layer := range layers {
			if layer.Err != nil {
				return fmt.Errorf("error reading layer[%d]: %w", layer.Index, layer.Err)
			}
			layerUnpacker := imageLayerUnpacker(layer)
			if err := layerUnpacker.Unpack(ctx, dest, layer, applyOpts...); err != nil {
				return fmt.Errorf("unpacking layer: %w", err)
			}
			l.Info("applied layer", "layer", layer.Index)
		}
		if err := fsutil.SetReadOnlyRecursive(dest); err != nil {
			return fmt.Errorf("error making unpack directory read-only: %w", err)
		}
		return nil
	}(); err != nil {
		return nil, time.Time{}, errors.Join(err, fsutil.DeleteReadOnlyRecursive(dest))
	}
	modTime, err := fsutil.GetDirectoryModTime(dest)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("error getting mod time of unpack directory: %w", err)
	}
	return os.DirFS(dest), modTime, nil
}

func storeChartLayer(path string, layer LayerData) error {
	if layer.Err != nil {
		return fmt.Errorf("error found in layer data: %w", layer.Err)
	}
	data, err := io.ReadAll(layer.Reader)
	if err != nil {
		return fmt.Errorf("error reading layer[%d]: %w", layer.Index, err)
	}
	meta := new(chart.Metadata)
	_, err = inspectChart(data, meta)
	if err != nil {
		return fmt.Errorf("inspecting chart layer: %w", err)
	}
	chart, err := renameio.TempFile("",
		filepath.Join(path,
			fmt.Sprintf("%s-%s.tgz", meta.Name, meta.Version),
		),
	)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() {
		_ = chart.Cleanup()
	}()
	if _, err := io.Copy(chart, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("copying chart archive: %w", err)
	}
	_, err = chart.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("seek chart archive start: %w", err)
	}
	return chart.CloseAtomicallyReplace()
}

func (a *diskCache) Delete(_ context.Context, ownerID string) error {
	return fsutil.DeleteReadOnlyRecursive(a.ownerIDPath(ownerID))
}

func (a *diskCache) GarbageCollect(_ context.Context, ownerID string, keep reference.Canonical) error {
	ownerIDPath := a.ownerIDPath(ownerID)
	dirEntries, err := os.ReadDir(ownerIDPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("error reading owner directory: %w", err)
	}

	foundKeep := false
	dirEntries = slices.DeleteFunc(dirEntries, func(entry os.DirEntry) bool {
		found := entry.Name() == keep.Digest().String()
		if found {
			foundKeep = true
		}
		return found
	})

	for _, dirEntry := range dirEntries {
		if err := fsutil.DeleteReadOnlyRecursive(filepath.Join(ownerIDPath, dirEntry.Name())); err != nil {
			return fmt.Errorf("error removing entry %s: %w", dirEntry.Name(), err)
		}
	}

	if !foundKeep {
		if err := fsutil.DeleteReadOnlyRecursive(ownerIDPath); err != nil {
			return fmt.Errorf("error deleting unused owner data: %w", err)
		}
	}
	return nil
}
