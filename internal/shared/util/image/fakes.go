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
	ImageFS fs.FS
	Ref     reference.Canonical
	ModTime time.Time
	Error   error
}

func (ms *FakePuller) Pull(_ context.Context, _, _ string, _ Cache) (fs.FS, reference.Canonical, time.Time, error) {
	if ms.Error != nil {
		return nil, nil, time.Time{}, ms.Error
	}

	return ms.ImageFS, ms.Ref, ms.ModTime, nil
}

var _ Cache = (*FakeCache)(nil)

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
