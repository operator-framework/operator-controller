package image

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/containerd/containerd/archive"
	"github.com/opencontainers/go-digest"
	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/docker/reference"
	"sigs.k8s.io/controller-runtime/pkg/log"

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

const (
	catalogCreatedFileSuffix = ".catalog-created"
	catalogRollbackStateFile = ".catalog-rollback-state"
	unknownCatalogCreated    = "unknown"
)

// CatalogRollbackProtector validates and records catalog publication dates.
type CatalogRollbackProtector interface {
	ValidateCatalog(context.Context, string, string, reference.Canonical) error
	AcceptCatalog(context.Context, string, string, reference.Canonical) error
}

// CatalogRollbackError reports an image that would replace a catalog with an
// older OCI image config creation timestamp.
type CatalogRollbackError struct {
	CandidateDigest  string
	CandidateCreated time.Time
	AcceptedDigest   string
	AcceptedCreated  time.Time
}

func (e *CatalogRollbackError) Error() string {
	return fmt.Sprintf("catalog image %s created at %s is not newer than cached catalog image %s created at %s", e.CandidateDigest, e.CandidateCreated.Format(time.RFC3339Nano), e.AcceptedDigest, e.AcceptedCreated.Format(time.RFC3339Nano))
}

func CatalogCache(basePath string) Cache {
	return &diskCache{
		basePath:            basePath,
		filterFunc:          filterForCatalogImage(),
		storeCatalogCreated: true,
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
	basePath            string
	filterFunc          func(context.Context, reference.Named, ocispecv1.Image) (archive.Filter, error)
	storeCatalogCreated bool
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
	if a.storeCatalogCreated {
		if _, err := os.Stat(a.catalogCreatedPath(ownerID, canonicalRef.Digest())); errors.Is(err, os.ErrNotExist) {
			l.Info("cached catalog has no creation metadata; repulling", "digest", canonicalRef.Digest())
			return nil, time.Time{}, fsutil.DeleteReadOnlyRecursive(unpackPath)
		} else if err != nil {
			return nil, time.Time{}, fmt.Errorf("error checking cached catalog creation metadata: %w", err)
		}
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

// catalogCreatedPath returns the metadata path for a cached catalog image's creation timestamp.
func (a *diskCache) catalogCreatedPath(ownerID string, digest digest.Digest) string {
	return filepath.Join(a.ownerIDPath(ownerID), digest.String()+catalogCreatedFileSuffix)
}

// catalogRollbackStatePath returns the path for the accepted catalog publication state.
func (a *diskCache) catalogRollbackStatePath(ownerID string) string {
	return filepath.Join(a.ownerIDPath(ownerID), catalogRollbackStateFile)
}

// catalogCreated returns a usable cached creation timestamp, if one is available.
func (a *diskCache) catalogCreated(ownerID string, canonicalRef reference.Canonical) (time.Time, error) {
	data, err := os.ReadFile(a.catalogCreatedPath(ownerID, canonicalRef.Digest()))
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("error reading cached catalog created timestamp: %w", err)
	}
	if string(data) == unknownCatalogCreated {
		return time.Time{}, nil
	}
	created, err := time.Parse(time.RFC3339Nano, string(data))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cached catalog created timestamp for image %s: %w", canonicalRef, err)
	}
	if !hasUsableCatalogCreated(created) {
		return time.Time{}, nil
	}
	return created, nil
}

// hasUsableCatalogCreated reports whether a creation timestamp can order catalog publications.
func hasUsableCatalogCreated(created time.Time) bool {
	return created.After(time.Unix(0, 0).UTC())
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
			if _, err := archive.Apply(ctx, dest, layer.Reader, applyOpts...); err != nil {
				return fmt.Errorf("error applying layer[%d]: %w", layer.Index, err)
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
	if a.storeCatalogCreated {
		createdPath := a.catalogCreatedPath(ownerID, canonicalRef.Digest())
		created := unknownCatalogCreated
		if imgCfg.Created != nil {
			created = imgCfg.Created.Format(time.RFC3339Nano)
		}
		if err := a.storeCatalogMetadata(createdPath, ".catalog-created-", created); err != nil {
			return nil, time.Time{}, errors.Join(err, fsutil.DeleteReadOnlyRecursive(dest))
		}
	}
	return os.DirFS(dest), modTime, nil
}

// storeCatalogMetadata atomically replaces a cache metadata file.
func (a *diskCache) storeCatalogMetadata(path, prefix, value string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), prefix)
	if err != nil {
		return fmt.Errorf("error creating cached catalog metadata file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := fmt.Fprint(tmp, value); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("error writing cached catalog metadata: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("error closing cached catalog metadata file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("error storing cached catalog metadata: %w", err)
	}
	return nil
}

type catalogRollbackState struct {
	ref     string
	digest  string
	created time.Time
}

// loadCatalogRollbackState returns the accepted publication state for an owner, if present.
func (a *diskCache) loadCatalogRollbackState(ownerID string) (catalogRollbackState, bool, error) {
	data, err := os.ReadFile(a.catalogRollbackStatePath(ownerID))
	if errors.Is(err, os.ErrNotExist) {
		return catalogRollbackState{}, false, nil
	}
	if err != nil {
		return catalogRollbackState{}, false, fmt.Errorf("error reading cached catalog rollback state: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) != 3 {
		return catalogRollbackState{}, false, fmt.Errorf("invalid cached catalog rollback state")
	}
	var state catalogRollbackState
	state.ref, state.digest = lines[0], lines[1]
	state.created, err = time.Parse(time.RFC3339Nano, lines[2])
	if err != nil || state.ref == "" || state.digest == "" {
		return catalogRollbackState{}, false, fmt.Errorf("invalid cached catalog rollback state")
	}
	if !hasUsableCatalogCreated(state.created) {
		state.created = time.Time{}
	}
	return state, true, nil
}

// ValidateCatalog rejects an older catalog publication.
func (a *diskCache) ValidateCatalog(ctx context.Context, ownerID, ref string, canonicalRef reference.Canonical) error {
	candidateCreated, err := a.catalogCreated(ownerID, canonicalRef)
	if err != nil {
		return err
	}
	if candidateCreated.IsZero() {
		log.FromContext(ctx).Info("catalog creation timestamp is unavailable; rollback protection is not enforced", "digest", canonicalRef.Digest())
		return nil
	}
	state, found, err := a.loadCatalogRollbackState(ownerID)
	if err != nil {
		return err
	}
	if !found || state.ref != ref || state.digest == canonicalRef.Digest().String() {
		return nil
	}
	if !candidateCreated.After(state.created) {
		return &CatalogRollbackError{CandidateDigest: canonicalRef.Digest().String(), CandidateCreated: candidateCreated, AcceptedDigest: state.digest, AcceptedCreated: state.created}
	}
	return nil
}

// AcceptCatalog records the catalog publication that was accepted for the source reference.
func (a *diskCache) AcceptCatalog(_ context.Context, ownerID, ref string, canonicalRef reference.Canonical) error {
	created, err := a.catalogCreated(ownerID, canonicalRef)
	if err != nil {
		return err
	}
	if created.IsZero() {
		previous, found, err := a.loadCatalogRollbackState(ownerID)
		if err != nil {
			return err
		}
		if found && previous.ref == ref && !previous.created.IsZero() {
			created = previous.created
		}
	}
	state := fmt.Sprintf("%s\n%s\n%s", ref, canonicalRef.Digest(), created.Format(time.RFC3339Nano))
	return a.storeCatalogMetadata(a.catalogRollbackStatePath(ownerID), ".catalog-rollback-state-", state)
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
		return found || entry.Name() == keep.Digest().String()+catalogCreatedFileSuffix || entry.Name() == catalogRollbackStateFile
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
