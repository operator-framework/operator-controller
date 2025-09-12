package image

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"github.com/containerd/containerd/archive"
	"github.com/containers/image/v5/docker/reference"
	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/registry"

	fsutil "github.com/operator-framework/operator-controller/internal/shared/util/fs"
)

func TestDiskCacheFetch(t *testing.T) {
	const myOwner = "myOwner"
	myRef := mustParseCanonical(t, "my.registry.io/ns/repo@sha256:5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03")

	testCases := []struct {
		name    string
		ownerID string
		ref     reference.Canonical
		setup   func(*testing.T, *diskCache)
		expect  func(*testing.T, *diskCache, fs.FS, time.Time, error)
	}{
		{
			name:    "all zero-values when owner does not exist",
			ownerID: myOwner,
			ref:     myRef,
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				assert.Nil(t, fsys)
				assert.Zero(t, modTime)
				assert.NoError(t, err)
			},
		},
		{
			name:    "all zero values when digest does not exist for owner",
			ownerID: myOwner,
			ref:     myRef,
			setup: func(t *testing.T, cache *diskCache) {
				require.NoError(t, os.MkdirAll(cache.ownerIDPath(myOwner), 0777))
			},
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				assert.Nil(t, fsys)
				assert.Zero(t, modTime)
				assert.NoError(t, err)
			},
		},
		{
			name:    "owners do not share data",
			ownerID: myOwner,
			ref:     myRef,
			setup: func(t *testing.T, cache *diskCache) {
				require.NoError(t, os.MkdirAll(cache.unpackPath("otherOwner", myRef.Digest()), 0777))
			},
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				assert.Nil(t, fsys)
				assert.Zero(t, modTime)
				assert.NoError(t, err)
			},
		},
		{
			name:    "permission error when owner directory cannot be read",
			ownerID: myOwner,
			ref:     myRef,
			setup: func(t *testing.T, cache *diskCache) {
				ownerIDPath := cache.ownerIDPath(myOwner)
				require.NoError(t, os.MkdirAll(ownerIDPath, 0700))
				require.NoError(t, os.Chmod(ownerIDPath, 0000))
			},
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				assert.Nil(t, fsys)
				assert.Zero(t, modTime)
				assert.ErrorIs(t, err, os.ErrPermission)
			},
		},
		{
			name:    "unexpected contents for a reference are deleted, zero values returned",
			ownerID: myOwner,
			ref:     myRef,
			setup: func(t *testing.T, cache *diskCache) {
				ownerIDPath := cache.ownerIDPath(myOwner)
				require.NoError(t, os.MkdirAll(ownerIDPath, 0700))
				require.NoError(t, os.WriteFile(cache.unpackPath(myOwner, myRef.Digest()), []byte{}, 0600))
			},
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				assert.Nil(t, fsys)
				assert.Zero(t, modTime)
				assert.NoError(t, err) // nolint:testifylint
				assert.NoFileExists(t, cache.unpackPath(myOwner, myRef.Digest()))
			},
		},
		{
			name:    "digest exists for owner",
			ownerID: myOwner,
			ref:     myRef,
			setup: func(t *testing.T, cache *diskCache) {
				unpackPath := cache.unpackPath(myOwner, myRef.Digest())
				require.NoError(t, os.MkdirAll(cache.ownerIDPath(myOwner), 0700))
				require.NoError(t, os.MkdirAll(unpackPath, 0700))
				require.NoError(t, os.WriteFile(filepath.Join(unpackPath, "my-file"), []byte("my-data"), 0600))
			},
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				require.NoError(t, err)

				// Verify fsys
				data, err := fs.ReadFile(fsys, "my-file")
				require.NoError(t, err)
				assert.Equal(t, "my-data", string(data))

				// Verify modTime
				dirStat, err := os.Stat(cache.unpackPath(myOwner, myRef.Digest()))
				assert.Equal(t, dirStat.ModTime(), modTime)

				// Verify no error
				assert.NoError(t, err)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dc := &diskCache{basePath: t.TempDir()}
			if tc.setup != nil {
				tc.setup(t, dc)
			}
			fsys, modTime, err := dc.Fetch(context.Background(), tc.ownerID, tc.ref)
			require.NotNil(t, tc.expect, "test case must include an expect function")
			tc.expect(t, dc, fsys, modTime, err)
			require.NoError(t, fsutil.SetWritableRecursive(dc.basePath))
		})
	}
}

func TestDiskCacheStore_HelmChart(t *testing.T) {
	const myOwner = "myOwner"
	myCanonicalRef := mustParseCanonical(t, "my.registry.io/ns/chart@sha256:5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03")
	myTaggedRef, err := reference.WithTag(reference.TrimNamed(myCanonicalRef), "test-tag")
	require.NoError(t, err)

	testCases := []struct {
		name         string
		ownerID      string
		srcRef       reference.Named
		canonicalRef reference.Canonical
		imgConfig    ocispecv1.Image
		layers       iter.Seq[LayerData]
		filterFunc   func(context.Context, reference.Named, ocispecv1.Image) (archive.Filter, error)
		setup        func(*testing.T, *diskCache)
		expect       func(*testing.T, *diskCache, fs.FS, time.Time, error)
	}{
		{
			name:         "returns no error if layer read contains helm chart",
			ownerID:      myOwner,
			srcRef:       myTaggedRef,
			canonicalRef: myCanonicalRef,
			layers: func() iter.Seq[LayerData] {
				testChart := mockHelmChartTgz(t,
					[]fileContent{
						{
							name:    "testchart/Chart.yaml",
							content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
						},
						{
							name:    "testchart/templates/deployment.yaml",
							content: []byte("kind: Deployment\napiVersion: apps/v1"),
						},
					},
				)
				return func(yield func(LayerData) bool) {
					yield(LayerData{Reader: bytes.NewBuffer(testChart), MediaType: registry.ChartLayerMediaType})
				}
			}(),
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				require.NoError(t, err)
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dc := &diskCache{
				basePath:   t.TempDir(),
				filterFunc: tc.filterFunc,
			}
			if tc.setup != nil {
				tc.setup(t, dc)
			}
			fsys, modTime, err := dc.Store(context.Background(), tc.ownerID, tc.srcRef, tc.canonicalRef, tc.imgConfig, tc.layers)
			require.NotNil(t, tc.expect, "test case must include an expect function")
			tc.expect(t, dc, fsys, modTime, err)
			require.NoError(t, fsutil.DeleteReadOnlyRecursive(dc.basePath))
		})
	}
}

func TestDiskCacheStore(t *testing.T) {
	const myOwner = "myOwner"
	myCanonicalRef := mustParseCanonical(t, "my.registry.io/ns/repo@sha256:5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03")
	myTaggedRef, err := reference.WithTag(reference.TrimNamed(myCanonicalRef), "test-tag")
	require.NoError(t, err)

	myUID := os.Getuid()
	myGID := os.Getgid()

	testCases := []struct {
		name         string
		ownerID      string
		srcRef       reference.Named
		canonicalRef reference.Canonical
		imgConfig    ocispecv1.Image
		layers       iter.Seq[LayerData]
		filterFunc   func(context.Context, reference.Named, ocispecv1.Image) (archive.Filter, error)
		setup        func(*testing.T, *diskCache)
		expect       func(*testing.T, *diskCache, fs.FS, time.Time, error)
	}{
		{
			name: "returns error when filter func fails",
			filterFunc: func(context.Context, reference.Named, ocispecv1.Image) (archive.Filter, error) {
				return nil, errors.New("filterfunc error")
			},
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				assert.Nil(t, fsys)
				assert.Zero(t, modTime)
				assert.ErrorContains(t, err, "filterfunc error")
			},
		},
		{
			name:         "returns permission error when base path is not writeable",
			ownerID:      myOwner,
			srcRef:       myTaggedRef,
			canonicalRef: myCanonicalRef,
			setup: func(t *testing.T, cache *diskCache) {
				require.NoError(t, os.Chmod(cache.basePath, 0400))
			},
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				assert.ErrorIs(t, err, os.ErrPermission)
			},
		},
		{
			name:         "returns error if layer data contains error",
			ownerID:      myOwner,
			srcRef:       myTaggedRef,
			canonicalRef: myCanonicalRef,
			layers: func(yield func(LayerData) bool) {
				yield(LayerData{Err: errors.New("layer error")})
			},
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				assert.ErrorContains(t, err, "layer error")
			},
		},
		{
			name:         "returns error if layer read returns error",
			ownerID:      myOwner,
			srcRef:       myTaggedRef,
			canonicalRef: myCanonicalRef,
			layers: func(yield func(LayerData) bool) {
				yield(LayerData{Reader: strings.NewReader("hello :)")})
			},
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				assert.ErrorContains(t, err, "error applying layer")
			},
		},
		{
			name:         "no error and an empty FS returned when there are no layers",
			ownerID:      myOwner,
			srcRef:       myTaggedRef,
			canonicalRef: myCanonicalRef,
			layers:       layerFSIterator(),
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				require.NoError(t, err)

				entries, err := fs.ReadDir(fsys, ".")
				assert.NoError(t, err) // nolint:testifylint
				assert.Empty(t, entries)
			},
		},
		{
			name:         "multiple layers with whiteouts are stored as expected",
			ownerID:      myOwner,
			srcRef:       myTaggedRef,
			canonicalRef: myCanonicalRef,
			layers: layerFSIterator(
				fstest.MapFS{
					"foo":  &fstest.MapFile{Data: []byte("foo_layer1"), Mode: 0600},
					"bar":  &fstest.MapFile{Data: []byte("bar_layer1"), Mode: 0600},
					"fizz": &fstest.MapFile{Data: []byte("fizz_layer1"), Mode: 0600},
				},
				fstest.MapFS{
					"foo":     &fstest.MapFile{Data: []byte("foo_layer2"), Mode: 0600},
					".wh.bar": &fstest.MapFile{Mode: 0600},
					"baz":     &fstest.MapFile{Data: []byte("baz_layer2"), Mode: 0600},
				},
			),
			filterFunc: func(ctx context.Context, named reference.Named, image ocispecv1.Image) (archive.Filter, error) {
				return forceOwnershipRWX(), nil
			},
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				require.NoError(t, err)
				require.NotNil(t, fsys)

				fooData, fooErr := fs.ReadFile(fsys, "foo")
				barData, barErr := fs.ReadFile(fsys, "bar")
				bazData, bazErr := fs.ReadFile(fsys, "baz")
				fizzData, fizzErr := fs.ReadFile(fsys, "fizz")

				assert.Equal(t, "foo_layer2", string(fooData))
				assert.NoError(t, fooErr) //nolint:testifylint

				assert.Equal(t, "baz_layer2", string(bazData))
				assert.NoError(t, bazErr) //nolint:testifylint

				assert.Empty(t, barData)
				assert.ErrorIs(t, barErr, fs.ErrNotExist) //nolint:testifylint

				assert.Equal(t, "fizz_layer1", string(fizzData))
				assert.NoError(t, fizzErr) //nolint:testifylint
			},
		},
		{
			name:         "uses filter",
			ownerID:      myOwner,
			srcRef:       myTaggedRef,
			canonicalRef: myCanonicalRef,
			filterFunc: func(context.Context, reference.Named, ocispecv1.Image) (archive.Filter, error) {
				return allFilters(
					func(h *tar.Header) (bool, error) {
						if h.Name == "foo" {
							return false, nil
						}
						h.Name += ".txt"
						return true, nil
					},
					forceOwnershipRWX(),
				), nil
			},
			layers: layerFSIterator(
				fstest.MapFS{
					"foo": &fstest.MapFile{Data: []byte("foo_layer1"), Mode: 0600},
					"bar": &fstest.MapFile{Data: []byte("bar_layer1"), Mode: 0600},
				},
			),
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				require.NoError(t, err)

				_, fooErr := fs.Stat(fsys, "foo")
				assert.ErrorIs(t, fooErr, fs.ErrNotExist) //nolint:testifylint

				_, barErr := fs.Stat(fsys, "bar")
				assert.ErrorIs(t, barErr, fs.ErrNotExist) //nolint:testifylint

				_, barTxtStat := fs.Stat(fsys, "bar.txt")
				assert.NoError(t, barTxtStat) //nolint:testifylint
			},
		},
		{
			name:         "uses bundle filter",
			ownerID:      myOwner,
			srcRef:       myTaggedRef,
			canonicalRef: myCanonicalRef,
			filterFunc:   filterForBundleImage(),
			layers: layerFSIterator(
				fstest.MapFS{
					"foo": &fstest.MapFile{Data: []byte("foo_layer1"), Mode: 0000},
					"bar": &fstest.MapFile{Data: []byte("bar_layer1"), Mode: 0000},
				},
			),
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				require.NoError(t, err)

				fooStat, fooErr := fs.Stat(fsys, "foo")
				barStat, barErr := fs.Stat(fsys, "bar")

				// You may have expected 0700 here, but after the files are stored,
				// the cache recursively sets them as read-only. The fact that these
				// files even exist proves that the filter executed properly because
				// they are UID/GID: 0/0 in the fs layer, which we would not have
				// been permitted to write to disk
				assert.Equal(t, fs.FileMode(0400).String(), fooStat.Mode().String())
				assert.Equal(t, fs.FileMode(0400).String(), barStat.Mode().String())
				assert.Equal(t, myUID, int(fooStat.Sys().(*syscall.Stat_t).Uid))
				assert.Equal(t, myGID, int(fooStat.Sys().(*syscall.Stat_t).Gid))
				assert.Equal(t, myUID, int(barStat.Sys().(*syscall.Stat_t).Uid))
				assert.Equal(t, myGID, int(barStat.Sys().(*syscall.Stat_t).Gid))

				assert.NoError(t, fooErr)
				assert.NoError(t, barErr)
			},
		},
		{
			name:         "fails if catalog filter cannot find expected image label",
			ownerID:      myOwner,
			srcRef:       myTaggedRef,
			canonicalRef: myCanonicalRef,
			filterFunc:   filterForCatalogImage(),
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				require.ErrorContains(t, err, "catalog image is missing the required label")
			},
		},
		{
			name:         "catalog filter includes only files under label's directory tree",
			ownerID:      myOwner,
			srcRef:       myTaggedRef,
			canonicalRef: myCanonicalRef,
			filterFunc:   filterForCatalogImage(),
			imgConfig: ocispecv1.Image{
				Config: ocispecv1.ImageConfig{
					Labels: map[string]string{
						ConfigDirLabel: "my-fav-configs",
					},
				},
			},
			layers: layerFSIterator(
				fstest.MapFS{
					"foo":                         &fstest.MapFile{Data: []byte("foo_layer1"), Mode: 0000},
					"my-fav-configs/catalog.json": &fstest.MapFile{Data: []byte(`{}`), Mode: 0000},
				},
			),
			expect: func(t *testing.T, cache *diskCache, fsys fs.FS, modTime time.Time, err error) {
				require.NoError(t, err)

				_, fooErr := fs.Stat(fsys, "foo")
				assert.ErrorIs(t, fooErr, fs.ErrNotExist) //nolint:testifylint

				catalogDataStat, catalogDataErr := fs.Stat(fsys, "my-fav-configs/catalog.json")
				assert.NoError(t, catalogDataErr) // nolint:testifylint

				// You may have expected 0700 here, but after the files are stored,
				// the cache recursively sets them as read-only. The fact that these
				// files even exist proves that the filter executed properly because
				// they are UID/GID: 0/0 in the fs layer, which we would not have
				// been permitted to write to disk
				assert.Equal(t, fs.FileMode(0400).String(), catalogDataStat.Mode().String())
				assert.Equal(t, myUID, int(catalogDataStat.Sys().(*syscall.Stat_t).Uid))
				assert.Equal(t, myGID, int(catalogDataStat.Sys().(*syscall.Stat_t).Gid))
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dc := &diskCache{
				basePath:   t.TempDir(),
				filterFunc: tc.filterFunc,
			}
			if tc.setup != nil {
				tc.setup(t, dc)
			}
			fsys, modTime, err := dc.Store(context.Background(), tc.ownerID, tc.srcRef, tc.canonicalRef, tc.imgConfig, tc.layers)
			require.NotNil(t, tc.expect, "test case must include an expect function")
			tc.expect(t, dc, fsys, modTime, err)
			require.NoError(t, fsutil.DeleteReadOnlyRecursive(dc.basePath))
		})
	}
}

func TestDiskCacheDelete(t *testing.T) {
	const myOwner = "myOwner"

	testCases := []struct {
		name    string
		ownerID string
		setup   func(*testing.T, *diskCache)
		expect  func(*testing.T, *diskCache, error)
	}{
		{
			name:    "no error when owner does not exist",
			ownerID: myOwner,
			expect: func(t *testing.T, cache *diskCache, err error) {
				assert.NoError(t, err) // nolint:testifylint
				assert.NoDirExists(t, cache.ownerIDPath(myOwner))
			},
		},
		{
			name:    "does not delete a different owner",
			ownerID: myOwner,
			setup: func(t *testing.T, cache *diskCache) {
				require.NoError(t, os.MkdirAll(cache.ownerIDPath("otherOwner"), 0500))
			},
			expect: func(t *testing.T, cache *diskCache, err error) {
				assert.NoError(t, err) //nolint:testifylint
				assert.DirExists(t, cache.ownerIDPath("otherOwner"))
				assert.NoDirExists(t, cache.ownerIDPath(myOwner))
			},
		},
		{
			name:    "deletes read-only owner",
			ownerID: myOwner,
			setup: func(t *testing.T, cache *diskCache) {
				ownerIDPath := cache.ownerIDPath(myOwner)
				require.NoError(t, os.MkdirAll(ownerIDPath, 0700))
				require.NoError(t, os.MkdirAll(cache.unpackPath(myOwner, "subdir"), 0700))
				require.NoError(t, fsutil.SetReadOnlyRecursive(ownerIDPath))
			},
			expect: func(t *testing.T, cache *diskCache, err error) {
				assert.NoError(t, err) // nolint:testifylint
				assert.NoDirExists(t, cache.ownerIDPath(myOwner))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dc := &diskCache{basePath: t.TempDir()}
			if tc.setup != nil {
				tc.setup(t, dc)
			}
			err := dc.Delete(context.Background(), tc.ownerID)
			require.NotNil(t, tc.expect, "test case must include an expect function")
			tc.expect(t, dc, err)
			require.NoError(t, fsutil.SetWritableRecursive(dc.basePath))
		})
	}
}

func TestDiskCacheGarbageCollection(t *testing.T) {
	const myOwner = "myOwner"
	myRef := mustParseCanonical(t, "my.registry.io/ns/repo@sha256:5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03")

	testCases := []struct {
		name    string
		ownerID string
		keep    reference.Canonical
		setup   func(*testing.T, *diskCache)
		expect  func(*testing.T, *diskCache, error)
	}{
		{
			name:    "error when owner ID path is not readable",
			ownerID: myOwner,
			keep:    myRef,
			setup: func(t *testing.T, cache *diskCache) {
				ownerPath := cache.ownerIDPath(myOwner)
				require.NoError(t, os.MkdirAll(ownerPath, 0700))
				require.NoError(t, os.Chmod(ownerPath, 0000))
			},
			expect: func(t *testing.T, cache *diskCache, err error) {
				assert.ErrorIs(t, err, os.ErrPermission)
			},
		},
		{
			name:    "error when owner ID path is not writeable and contains gc-able content",
			ownerID: myOwner,
			keep:    myRef,
			setup: func(t *testing.T, cache *diskCache) {
				ownerPath := cache.ownerIDPath(myOwner)
				require.NoError(t, os.MkdirAll(ownerPath, 0700))
				require.NoError(t, os.MkdirAll(cache.unpackPath(myOwner, "subdir"), 0700))
				require.NoError(t, os.Chmod(ownerPath, 0500))
			},
			expect: func(t *testing.T, cache *diskCache, err error) {
				assert.ErrorIs(t, err, os.ErrPermission)
			},
		},
		{
			name:    "error when base path is not writeable and contains gc-able content",
			ownerID: myOwner,
			keep:    myRef,
			setup: func(t *testing.T, cache *diskCache) {
				ownerPath := cache.ownerIDPath(myOwner)
				require.NoError(t, os.MkdirAll(ownerPath, 0700))
				require.NoError(t, os.MkdirAll(cache.unpackPath(myOwner, "subdir"), 0700))
				require.NoError(t, os.Chmod(cache.basePath, 0500))
			},
			expect: func(t *testing.T, cache *diskCache, err error) {
				assert.ErrorIs(t, err, os.ErrPermission)
			},
		},
		{
			name:    "no error when owner does not exist",
			ownerID: myOwner,
			keep:    myRef,
			expect: func(t *testing.T, cache *diskCache, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name:    "no error when owner has no contents, deletes owner dir",
			ownerID: myOwner,
			keep:    myRef,
			setup: func(t *testing.T, cache *diskCache) {
				ownerPath := cache.ownerIDPath(myOwner)
				require.NoError(t, os.MkdirAll(ownerPath, 0700))
				require.NoError(t, fsutil.SetReadOnlyRecursive(ownerPath))
			},
			expect: func(t *testing.T, cache *diskCache, err error) {
				assert.NoError(t, err) //nolint:testifylint
				assert.NoDirExists(t, cache.ownerIDPath(myOwner))
			},
		},
		{
			name:    "no error when owner does not have keep reference, deletes owner dir",
			ownerID: myOwner,
			keep:    myRef,
			setup: func(t *testing.T, cache *diskCache) {
				unpackPath := cache.unpackPath(myOwner, "subdir")
				require.NoError(t, os.MkdirAll(cache.ownerIDPath(myOwner), 0700))
				require.NoError(t, os.MkdirAll(unpackPath, 0700))
				require.NoError(t, fsutil.SetReadOnlyRecursive(unpackPath))
			},
			expect: func(t *testing.T, cache *diskCache, err error) {
				assert.NoError(t, err) //nolint:testifylint
				assert.NoDirExists(t, cache.ownerIDPath(myOwner))
			},
		},
		{
			name:    "deletes everything _except_ keep's data",
			ownerID: myOwner,
			keep:    myRef,
			setup: func(t *testing.T, cache *diskCache) {
				otherPath := cache.unpackPath(myOwner, "subdir")
				unpackPath := cache.unpackPath(myOwner, myRef.Digest())
				require.NoError(t, os.MkdirAll(cache.ownerIDPath(myOwner), 0700))
				require.NoError(t, os.MkdirAll(otherPath, 0700))
				require.NoError(t, os.MkdirAll(unpackPath, 0700))
				require.NoError(t, fsutil.SetReadOnlyRecursive(otherPath))
				require.NoError(t, fsutil.SetReadOnlyRecursive(unpackPath))
			},
			expect: func(t *testing.T, cache *diskCache, err error) {
				assert.NoError(t, err) //nolint:testifylint
				assert.DirExists(t, cache.unpackPath(myOwner, myRef.Digest()))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dc := &diskCache{basePath: t.TempDir()}
			if tc.setup != nil {
				tc.setup(t, dc)
			}
			err := dc.GarbageCollect(context.Background(), tc.ownerID, tc.keep)
			require.NotNil(t, tc.expect, "test case must include an expect function")
			tc.expect(t, dc, err)
			require.NoError(t, fsutil.SetWritableRecursive(dc.basePath))
		})
	}
}

func Test_storeChartLayer(t *testing.T) {
	tmp := t.TempDir()
	type args struct {
		path string
		data LayerData
	}
	type want struct {
		errStr string
	}

	tests := []struct {
		name string
		args args
		want want
	}{
		{
			name: "store chart layer to given path",
			args: args{
				path: tmp,
				data: LayerData{
					Index:     0,
					MediaType: registry.ChartLayerMediaType,
					Reader: bytes.NewBuffer(mockHelmChartTgz(t,
						[]fileContent{
							{
								name:    "testchart/Chart.yaml",
								content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
							},
							{
								name:    "testchart/templates/deployment.yaml",
								content: []byte("kind: Deployment\napiVersion: apps/v1"),
							},
						},
					)),
				},
			},
		},
		{
			name: "store invalid chart layer",
			args: args{
				path: tmp,
				data: LayerData{
					Index:     0,
					MediaType: registry.ChartLayerMediaType,
					Reader: bytes.NewBuffer(mockHelmChartTgz(t,
						[]fileContent{
							{
								name:    "testchart/Chart.yaml",
								content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
							},
							{
								name:    "testchart/deployment.yaml",
								content: []byte("kind: Deployment\napiVersion: apps/v1"),
							},
						},
					)),
				},
			},
		},
		{
			name: "store existing from dummy reader",
			args: args{
				path: tmp,
				data: LayerData{
					Index:     0,
					MediaType: registry.ChartLayerMediaType,
					Reader:    &dummyReader{},
				},
			},
			want: want{
				errStr: "error reading layer[0]: something went wrong",
			},
		},
		{
			name: "handle chart layer data",
			args: args{
				path: tmp,
				data: LayerData{
					Index:     0,
					MediaType: registry.ChartLayerMediaType,
					Err:       fmt.Errorf("invalid layer data"),
					Reader: bytes.NewBuffer(mockHelmChartTgz(t,
						[]fileContent{
							{
								name:    "testchart/Chart.yaml",
								content: []byte("apiVersion: v2\nname: testchart\nversion: 0.1.0"),
							},
							{
								name:    "testchart/deployment.yaml",
								content: []byte("kind: Deployment\napiVersion: apps/v1"),
							},
						},
					)),
				},
			},
			want: want{
				errStr: "error found in layer data: invalid layer data",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := storeChartLayer(tc.args.path, tc.args.data)
			if tc.want.errStr != "" {
				require.Error(t, err)
				require.EqualError(t, err, tc.want.errStr, "chart store error")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func mustParseCanonical(t *testing.T, s string) reference.Canonical {
	n, err := reference.ParseNamed(s)
	require.NoError(t, err)
	c, ok := n.(reference.Canonical)
	require.True(t, ok, "image reference must be canonical")
	return c
}

func layerFSIterator(layerFilesystems ...fs.FS) iter.Seq[LayerData] {
	return func(yield func(data LayerData) bool) {
		for i, fsys := range layerFilesystems {
			rc := fsTarReader(fsys)
			ld := LayerData{
				Reader: rc,
				Index:  i,
			}
			stop := !yield(ld)
			_ = rc.Close()
			if stop {
				return
			}
		}
	}
}

func fsTarReader(fsys fs.FS) io.ReadCloser {
	pr, pw := io.Pipe()
	tw := tar.NewWriter(pw)
	go func() {
		err := tw.AddFS(fsys)
		_ = pw.CloseWithError(err)
	}()
	return pr
}

type dummyReader struct{}

var _ io.Reader = &dummyReader{}

func (r *dummyReader) Read(p []byte) (int, error) {
	return 0, errors.New("something went wrong")
}
