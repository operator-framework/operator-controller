package image

import (
	"context"
	"io/fs"
	"iter"
	"time"

	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"go.podman.io/image/v5/docker/reference"
)

var _ Puller = (*MockPuller)(nil)

// MockPuller is a utility for mocking out a Puller interface
type MockPuller struct {
	ImageFS fs.FS
	Ref     reference.Canonical
	ModTime time.Time
	Error   error
}

func (ms *MockPuller) Pull(_ context.Context, _, _ string, _ Cache) (fs.FS, reference.Canonical, time.Time, error) {
	if ms.Error != nil {
		return nil, nil, time.Time{}, ms.Error
	}

	return ms.ImageFS, ms.Ref, ms.ModTime, nil
}

var _ Cache = (*MockCache)(nil)

type MockCache struct {
	FetchFS      fs.FS
	FetchModTime time.Time
	FetchError   error

	StoreFS      fs.FS
	StoreModTime time.Time
	StoreError   error

	DeleteErr error

	GarbageCollectError error
}

func (m MockCache) Fetch(_ context.Context, _ string, _ reference.Canonical) (fs.FS, time.Time, error) {
	return m.FetchFS, m.FetchModTime, m.FetchError
}

func (m MockCache) Store(_ context.Context, _ string, _ reference.Named, _ reference.Canonical, _ ocispecv1.Image, _ iter.Seq[LayerData]) (fs.FS, time.Time, error) {
	return m.StoreFS, m.StoreModTime, m.StoreError
}

func (m MockCache) Delete(_ context.Context, _ string) error {
	return m.DeleteErr
}

func (m MockCache) GarbageCollect(_ context.Context, _ string, _ reference.Canonical) error {
	return m.GarbageCollectError
}
