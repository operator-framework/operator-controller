package image

import (
	"context"
	"io/fs"
	"iter"
	"time"

	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/docker/reference"
)

var _ Puller = (*FakePuller)(nil)

// FakePuller is a test fake that returns preconfigured values for the Puller interface
type FakePuller struct {
	ImageFS        fs.FS
	Ref            reference.Canonical
	ModTime        time.Time
	CatalogVersion int64
	Error          error
}

func (ms *FakePuller) Pull(_ context.Context, _, _ string, _ Cache) (fs.FS, reference.Canonical, time.Time, error) {
	if ms.Error != nil {
		return nil, nil, time.Time{}, ms.Error
	}

	return ms.ImageFS, ms.Ref, ms.ModTime, nil
}

func (ms *FakePuller) PullCatalog(ctx context.Context, ownerID, ref string, cache Cache) (fs.FS, reference.Canonical, time.Time, int64, error) {
	fsys, canonicalRef, modTime, err := ms.Pull(ctx, ownerID, ref, cache)
	return fsys, canonicalRef, modTime, ms.CatalogVersion, err
}

var _ Cache = (*FakeCache)(nil)

var _ CatalogVersionCache = (*FakeCatalogVersionCache)(nil)

type FakeCache struct {
	FetchFS      fs.FS
	FetchModTime time.Time
	FetchError   error

	StoreFS      fs.FS
	StoreModTime time.Time
	StoreError   error

	DeleteErr error

	GarbageCollectError error
}

func (m FakeCache) Fetch(_ context.Context, _ string, _ reference.Canonical) (fs.FS, time.Time, error) {
	return m.FetchFS, m.FetchModTime, m.FetchError
}

func (m FakeCache) Store(_ context.Context, _ string, _ reference.Named, _ reference.Canonical, _ ocispecv1.Image, _ iter.Seq[LayerData]) (fs.FS, time.Time, error) {
	return m.StoreFS, m.StoreModTime, m.StoreError
}

func (m FakeCache) Delete(_ context.Context, _ string) error {
	return m.DeleteErr
}

func (m FakeCache) GarbageCollect(_ context.Context, _ string, _ reference.Canonical) error {
	return m.GarbageCollectError
}

// FakeCatalogVersionCache is a FakeCache that also returns catalog publication metadata.
type FakeCatalogVersionCache struct {
	FakeCache
	Version      int64
	VersionError error
}

func (m FakeCatalogVersionCache) CatalogVersion(_ context.Context, _ string, _ reference.Canonical) (int64, error) {
	return m.Version, m.VersionError
}
