package image

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/containerd/containerd/archive"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/opencontainers/go-digest"
	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.podman.io/image/v5/docker/reference"
	"go.podman.io/image/v5/pkg/sysregistriesv2"
	"go.podman.io/image/v5/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	fsutil "github.com/operator-framework/operator-controller/internal/shared/util/fs"
)

const (
	testFileName     string = "test-file"
	testFileContents string = "test-content"
)

func TestContainersImagePuller_Pull(t *testing.T) {
	const myOwner = "myOwner"
	myTagRef, myCanonicalRef, shutdown := setupRegistry(t)
	defer shutdown()

	myModTime := time.Date(1985, 10, 25, 7, 53, 0, 0, time.FixedZone("PDT", -8*60*60))
	defaultContextFunc := func(context.Context) (*types.SystemContext, error) { return &types.SystemContext{}, nil }

	testCases := []struct {
		name        string
		ownerID     string
		srcRef      string
		cache       Cache
		contextFunc func(context.Context) (*types.SystemContext, error)
		expect      func(*testing.T, fs.FS, reference.Canonical, time.Time, error)
	}{
		{
			name:        "returns terminal error for invalid reference",
			ownerID:     myOwner,
			srcRef:      "invalid-src-ref",
			contextFunc: defaultContextFunc,
			expect: func(t *testing.T, fsys fs.FS, canonical reference.Canonical, modTime time.Time, err error) {
				require.ErrorContains(t, err, "error parsing image reference")
				require.ErrorIs(t, err, reconcile.TerminalError(nil))
			},
		},
		{
			name:        "returns terminal error if reference lacks tag or digest",
			ownerID:     myOwner,
			srcRef:      reference.TrimNamed(myTagRef).String(),
			contextFunc: defaultContextFunc,
			expect: func(t *testing.T, fsys fs.FS, canonical reference.Canonical, modTime time.Time, err error) {
				require.ErrorContains(t, err, "error creating reference")
				require.ErrorIs(t, err, reconcile.TerminalError(nil))
			},
		},
		{
			name:    "returns error if failure getting SystemContext",
			ownerID: myOwner,
			srcRef:  myCanonicalRef.String(),
			contextFunc: func(ctx context.Context) (*types.SystemContext, error) {
				return nil, errors.New("sourcecontextfunc error")
			},
			expect: func(t *testing.T, fsys fs.FS, canonical reference.Canonical, modTime time.Time, err error) {
				require.ErrorContains(t, err, "sourcecontextfunc error")
			},
		},
		{
			name:        "returns error if failure connecting to reference's registry",
			ownerID:     myOwner,
			srcRef:      myTagRef.String() + "-non-existent",
			contextFunc: defaultContextFunc,
			expect: func(t *testing.T, fsys fs.FS, canonical reference.Canonical, modTime time.Time, err error) {
				require.ErrorContains(t, err, "pinging container registry")
			},
		},
		{
			name:        "returns error if tag ref is not found",
			ownerID:     myOwner,
			srcRef:      myTagRef.String() + "-non-existent",
			contextFunc: buildSourceContextFunc(t, myTagRef),
			expect: func(t *testing.T, fsys fs.FS, canonical reference.Canonical, modTime time.Time, err error) {
				require.ErrorContains(t, err, "manifest unknown")
			},
		},
		{
			name:        "return error if cache fetch fails",
			ownerID:     myOwner,
			srcRef:      myCanonicalRef.String(),
			cache:       MockCache{FetchError: errors.New("fetch error")},
			contextFunc: defaultContextFunc,
			expect: func(t *testing.T, fsys fs.FS, canonical reference.Canonical, modTime time.Time, err error) {
				require.ErrorContains(t, err, "fetch error")
			},
		},
		{
			name:    "return canonical ref's data from cache, if present",
			ownerID: myOwner,
			srcRef:  myCanonicalRef.String(),
			cache: MockCache{
				FetchFS: fstest.MapFS{
					testFileName: &fstest.MapFile{Data: []byte(testFileContents)},
				},
				FetchModTime: myModTime,
			},
			contextFunc: buildSourceContextFunc(t, myCanonicalRef),
			expect: func(t *testing.T, fsys fs.FS, canonical reference.Canonical, modTime time.Time, err error) {
				require.NoError(t, err)
				actualFileData, err := fs.ReadFile(fsys, testFileName)
				require.NoError(t, err)
				assert.Equal(t, testFileContents, string(actualFileData))

				assert.Equal(t, myCanonicalRef.String(), canonical.String())
				assert.Equal(t, myModTime, modTime)
			},
		},
		{
			name:    "return tag ref's data from cache, if present",
			ownerID: myOwner,
			srcRef:  myTagRef.String(),
			cache: MockCache{
				FetchFS: fstest.MapFS{
					testFileName: &fstest.MapFile{Data: []byte(testFileContents)},
				},
				FetchModTime: myModTime,
			},
			contextFunc: buildSourceContextFunc(t, myTagRef),
			expect: func(t *testing.T, fsys fs.FS, canonical reference.Canonical, modTime time.Time, err error) {
				require.NoError(t, err)
				actualFileData, err := fs.ReadFile(fsys, testFileName)
				require.NoError(t, err)
				assert.Equal(t, testFileContents, string(actualFileData))

				assert.Equal(t, myCanonicalRef.String(), canonical.String())
				assert.Equal(t, myModTime, modTime)
			},
		},
		{
			name:    "returns error if failure storing content in cache",
			ownerID: myOwner,
			srcRef:  myCanonicalRef.String(),
			cache: MockCache{
				StoreError: errors.New("store error"),
			},
			contextFunc: buildSourceContextFunc(t, myCanonicalRef),
			expect: func(t *testing.T, fsys fs.FS, canonical reference.Canonical, modTime time.Time, err error) {
				require.ErrorContains(t, err, "store error")
			},
		},
		{
			name:    "returns stored data upon pull success",
			ownerID: myOwner,
			srcRef:  myTagRef.String(),
			cache: MockCache{
				StoreFS: fstest.MapFS{
					testFileName: &fstest.MapFile{Data: []byte(testFileContents)},
				},
				StoreModTime: myModTime,
			},
			contextFunc: buildSourceContextFunc(t, myTagRef),
			expect: func(t *testing.T, fsys fs.FS, canonical reference.Canonical, modTime time.Time, err error) {
				require.NoError(t, err)

				actualFileData, err := fs.ReadFile(fsys, testFileName)
				require.NoError(t, err)
				assert.Equal(t, testFileContents, string(actualFileData))

				assert.Equal(t, myCanonicalRef.String(), canonical.String())
				assert.Equal(t, myModTime, modTime)
			},
		},
		{
			name:    "returns error if cache garbage collection fails",
			ownerID: myOwner,
			srcRef:  myTagRef.String(),
			cache: MockCache{
				StoreFS: fstest.MapFS{
					testFileName: &fstest.MapFile{Data: []byte(testFileContents)},
				},
				StoreModTime:        myModTime,
				GarbageCollectError: errors.New("garbage collect error"),
			},
			contextFunc: buildSourceContextFunc(t, myTagRef),
			expect: func(t *testing.T, fsys fs.FS, canonical reference.Canonical, modTime time.Time, err error) {
				assert.Nil(t, fsys)
				assert.Nil(t, canonical)
				assert.Zero(t, modTime)
				assert.ErrorContains(t, err, "garbage collect error")
			},
		},
		{
			name:    "succeeds storing actual image contents using real cache",
			ownerID: myOwner,
			srcRef:  myTagRef.String(),
			cache: &diskCache{
				basePath: t.TempDir(),
				filterFunc: func(ctx context.Context, named reference.Named, image ocispecv1.Image) (archive.Filter, error) {
					return forceOwnershipRWX(), nil
				},
			},
			contextFunc: buildSourceContextFunc(t, myTagRef),
			expect: func(t *testing.T, fsys fs.FS, canonical reference.Canonical, modTime time.Time, err error) {
				require.NoError(t, err)
				actualFileData, err := fs.ReadFile(fsys, testFileName)
				require.NoError(t, err)
				assert.Equal(t, testFileContents, string(actualFileData))
				assert.Equal(t, myCanonicalRef.String(), canonical.String())

				// Don't assert modTime since it is an implementation detail
				// of the cache, which we are not testing here.
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			puller := ContainersImagePuller{
				SourceCtxFunc: tc.contextFunc,
			}
			fsys, canonicalRef, modTime, err := puller.Pull(context.Background(), tc.ownerID, tc.srcRef, tc.cache)
			require.NotNil(t, tc.expect, "expect function must be defined")
			tc.expect(t, fsys, canonicalRef, modTime, err)

			if dc, ok := tc.cache.(*diskCache); ok && dc.basePath != "" {
				require.NoError(t, fsutil.DeleteReadOnlyRecursive(dc.basePath))
			}
		})
	}
}

func setupRegistry(t *testing.T) (reference.NamedTagged, reference.Canonical, func()) {
	server := httptest.NewServer(registry.New())
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	// Generate an image with file contents
	img, err := crane.Image(map[string][]byte{testFileName: []byte(testFileContents)})
	require.NoError(t, err)

	imageTagRef, err := newReference(serverURL.Host, "test-repo/test-image", "test-tag")
	require.NoError(t, err)

	imgDigest, err := img.Digest()
	require.NoError(t, err)

	imageDigestRef, err := reference.WithDigest(reference.TrimNamed(imageTagRef), digest.Digest(imgDigest.String()))
	require.NoError(t, err)

	require.NoError(t, crane.Push(img, imageTagRef.String()))

	cleanup := func() {
		server.Close()
	}
	return imageTagRef, imageDigestRef, cleanup
}

func newReference(host, repo, tag string) (reference.NamedTagged, error) {
	ref, err := reference.ParseNamed(fmt.Sprintf("%s/%s", host, repo))
	if err != nil {
		return nil, err
	}
	return reference.WithTag(ref, tag)
}

func buildSourceContextFunc(t *testing.T, ref reference.Named) func(context.Context) (*types.SystemContext, error) {
	return func(ctx context.Context) (*types.SystemContext, error) {
		// Build a containers/image context that allows pulling from the test registry insecurely
		registriesConf := sysregistriesv2.V2RegistriesConf{Registries: []sysregistriesv2.Registry{
			{
				Prefix: reference.Domain(ref),
				Endpoint: sysregistriesv2.Endpoint{
					Location: reference.Domain(ref),
					Insecure: true,
				},
			},
		}}
		configDir := t.TempDir()
		registriesConfPath := filepath.Join(configDir, "registries.conf")
		f, err := os.Create(registriesConfPath)
		require.NoError(t, err)

		enc := toml.NewEncoder(f)
		require.NoError(t, enc.Encode(registriesConf))
		require.NoError(t, f.Close())

		// Create an insecure policy for testing to override any system-level policy
		// that might reject unsigned images
		policyPath := filepath.Join(configDir, "policy.json")
		insecurePolicy := `{"default":[{"type":"insecureAcceptAnything"}]}`
		require.NoError(t, os.WriteFile(policyPath, []byte(insecurePolicy), 0600))

		return &types.SystemContext{
			SystemRegistriesConfPath: registriesConfPath,
			SignaturePolicyPath:      policyPath,
		}, nil
	}
}
