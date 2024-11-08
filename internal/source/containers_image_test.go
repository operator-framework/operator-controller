package source_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/containers/image/v5/types"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	catalogdv1 "github.com/operator-framework/catalogd/api/v1"
	"github.com/operator-framework/catalogd/internal/source"
)

func TestImageRegistry(t *testing.T) {
	for _, tt := range []struct {
		name string
		// catalog is the Catalog passed to the Unpack function.
		// if the Catalog.Spec.Source.Image.Ref field is empty,
		// one is injected during test runtime to ensure it
		// points to the registry created for the test
		catalog             *catalogdv1.ClusterCatalog
		wantErr             bool
		terminal            bool
		image               v1.Image
		digestAlreadyExists bool
		oldDigestExists     bool
		// refType is the type of image ref this test
		// is using. Should be one of "tag","digest"
		refType string
	}{
		{
			name: ".spec.source.image is nil",
			catalog: &catalogdv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: catalogdv1.ClusterCatalogSpec{
					Source: catalogdv1.CatalogSource{
						Type:  catalogdv1.SourceTypeImage,
						Image: nil,
					},
				},
			},
			wantErr:  true,
			terminal: true,
			refType:  "tag",
		},
		{
			name: ".spec.source.image.ref is unparsable",
			catalog: &catalogdv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: catalogdv1.ClusterCatalogSpec{
					Source: catalogdv1.CatalogSource{
						Type: catalogdv1.SourceTypeImage,
						Image: &catalogdv1.ImageSource{
							Ref: "::)12-as^&8asd789A(::",
						},
					},
				},
			},
			wantErr:  true,
			terminal: true,
			refType:  "tag",
		},
		{
			name: "tag based, image is missing required label",
			catalog: &catalogdv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: catalogdv1.ClusterCatalogSpec{
					Source: catalogdv1.CatalogSource{
						Type: catalogdv1.SourceTypeImage,
						Image: &catalogdv1.ImageSource{
							Ref: "",
						},
					},
				},
			},
			wantErr: true,
			image: func() v1.Image {
				img, err := random.Image(20, 3)
				if err != nil {
					panic(err)
				}
				return img
			}(),
			refType: "tag",
		},
		{
			name: "digest based, image is missing required label",
			catalog: &catalogdv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: catalogdv1.ClusterCatalogSpec{
					Source: catalogdv1.CatalogSource{
						Type: catalogdv1.SourceTypeImage,
						Image: &catalogdv1.ImageSource{
							Ref: "",
						},
					},
				},
			},
			wantErr:  true,
			terminal: true,
			image: func() v1.Image {
				img, err := random.Image(20, 3)
				if err != nil {
					panic(err)
				}
				return img
			}(),
			refType: "digest",
		},
		{
			name: "image doesn't exist",
			catalog: &catalogdv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: catalogdv1.ClusterCatalogSpec{
					Source: catalogdv1.CatalogSource{
						Type: catalogdv1.SourceTypeImage,
						Image: &catalogdv1.ImageSource{
							Ref: "",
						},
					},
				},
			},
			wantErr: true,
			refType: "tag",
		},
		{
			name: "tag based image, digest already exists in cache",
			catalog: &catalogdv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: catalogdv1.ClusterCatalogSpec{
					Source: catalogdv1.CatalogSource{
						Type: catalogdv1.SourceTypeImage,
						Image: &catalogdv1.ImageSource{
							Ref: "",
						},
					},
				},
			},
			wantErr: false,
			image: func() v1.Image {
				img, err := random.Image(20, 3)
				if err != nil {
					panic(err)
				}
				return img
			}(),
			digestAlreadyExists: true,
			refType:             "tag",
		},
		{
			name: "digest based image, digest already exists in cache",
			catalog: &catalogdv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: catalogdv1.ClusterCatalogSpec{
					Source: catalogdv1.CatalogSource{
						Type: catalogdv1.SourceTypeImage,
						Image: &catalogdv1.ImageSource{
							Ref: "",
						},
					},
				},
			},
			wantErr:             false,
			digestAlreadyExists: true,
			refType:             "digest",
			image: func() v1.Image {
				img, err := random.Image(20, 3)
				if err != nil {
					panic(err)
				}
				return img
			}(),
		},
		{
			name: "old ref is cached",
			catalog: &catalogdv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: catalogdv1.ClusterCatalogSpec{
					Source: catalogdv1.CatalogSource{
						Type: catalogdv1.SourceTypeImage,
						Image: &catalogdv1.ImageSource{
							Ref: "",
						},
					},
				},
			},
			wantErr:         false,
			oldDigestExists: true,
			refType:         "tag",
			image: func() v1.Image {
				img, err := random.Image(20, 3)
				if err != nil {
					panic(err)
				}
				img, err = mutate.Config(img, v1.Config{
					Labels: map[string]string{
						source.ConfigDirLabel: "/configs",
					},
				})
				if err != nil {
					panic(err)
				}
				return img
			}(),
		},
		{
			name: "tag ref, happy path",
			catalog: &catalogdv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: catalogdv1.ClusterCatalogSpec{
					Source: catalogdv1.CatalogSource{
						Type: catalogdv1.SourceTypeImage,
						Image: &catalogdv1.ImageSource{
							Ref: "",
						},
					},
				},
			},
			wantErr: false,
			refType: "tag",
			image: func() v1.Image {
				img, err := random.Image(20, 3)
				if err != nil {
					panic(err)
				}
				img, err = mutate.Config(img, v1.Config{
					Labels: map[string]string{
						source.ConfigDirLabel: "/configs",
					},
				})
				if err != nil {
					panic(err)
				}
				return img
			}(),
		},
		{
			name: "digest ref, happy path",
			catalog: &catalogdv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: catalogdv1.ClusterCatalogSpec{
					Source: catalogdv1.CatalogSource{
						Type: catalogdv1.SourceTypeImage,
						Image: &catalogdv1.ImageSource{
							Ref: "",
						},
					},
				},
			},
			wantErr: false,
			refType: "digest",
			image: func() v1.Image {
				img, err := random.Image(20, 3)
				if err != nil {
					panic(err)
				}
				img, err = mutate.Config(img, v1.Config{
					Labels: map[string]string{
						source.ConfigDirLabel: "/configs",
					},
				})
				if err != nil {
					panic(err)
				}
				return img
			}(),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Create context, temporary cache directory,
			// and image registry source
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			testCache := t.TempDir()
			imgReg := &source.ContainersImageRegistry{
				BaseCachePath: testCache,
				SourceContextFunc: func(logger logr.Logger) (*types.SystemContext, error) {
					return &types.SystemContext{
						OCIInsecureSkipTLSVerify:    true,
						DockerInsecureSkipTLSVerify: types.OptionalBoolTrue,
					}, nil
				},
			}

			// Create a logger with a simple function-based LogSink that writes to the buffer
			var buf bytes.Buffer
			logger := funcr.New(func(prefix, args string) {
				buf.WriteString(fmt.Sprintf("%s %s\n", prefix, args))
			}, funcr.Options{Verbosity: 1})

			// Add the logger into the context which will later be used
			// in the Unpack function to get the logger
			ctx = log.IntoContext(ctx, logger)

			// Start a new server running an image registry
			srv := httptest.NewServer(registry.New())
			defer srv.Close()

			// parse the server url so we can grab just the host
			url, err := url.Parse(srv.URL)
			require.NoError(t, err)

			// Build the proper image name with {registry}/tt.imgName
			imgName, err := name.ParseReference(fmt.Sprintf("%s/%s", url.Host, "test-image:test"))
			require.NoError(t, err)

			// If an old digest should exist in the cache, create one
			oldDigestDir := filepath.Join(testCache, tt.catalog.Name, "olddigest")
			var oldDigestModTime time.Time
			if tt.oldDigestExists {
				require.NoError(t, os.MkdirAll(oldDigestDir, os.ModePerm))
				oldDigestDirStat, err := os.Stat(oldDigestDir)
				require.NoError(t, err)
				oldDigestModTime = oldDigestDirStat.ModTime()
			}

			var digest v1.Hash
			// if the test specifies a method that returns a v1.Image,
			// call it and push the image to the registry
			if tt.image != nil {
				digest, err = tt.image.Digest()
				require.NoError(t, err)

				// if the digest should already exist in the cache, create it
				if tt.digestAlreadyExists {
					err = os.MkdirAll(filepath.Join(testCache, tt.catalog.Name, digest.String()), os.ModePerm)
					require.NoError(t, err)
				}

				err = remote.Write(imgName, tt.image)
				require.NoError(t, err)

				// if the image ref should be a digest ref, make it so
				if tt.refType == "digest" {
					imgName, err = name.ParseReference(fmt.Sprintf("%s/%s", url.Host, "test-image@sha256:"+digest.Hex))
					require.NoError(t, err)
				}
			}

			// Inject the image reference if needed
			if tt.catalog.Spec.Source.Image != nil && tt.catalog.Spec.Source.Image.Ref == "" {
				tt.catalog.Spec.Source.Image.Ref = imgName.Name()
			}

			rs, err := imgReg.Unpack(ctx, tt.catalog)
			if !tt.wantErr {
				require.NoError(t, err)
				assert.Equal(t, fmt.Sprintf("%s@sha256:%s", imgName.Context().Name(), digest.Hex), rs.ResolvedSource.Image.Ref)
				assert.Equal(t, source.StateUnpacked, rs.State)

				unpackDir := filepath.Join(testCache, tt.catalog.Name, digest.String())
				assert.DirExists(t, unpackDir)
				unpackDirStat, err := os.Stat(unpackDir)
				require.NoError(t, err)

				entries, err := os.ReadDir(filepath.Join(testCache, tt.catalog.Name))
				require.NoError(t, err)
				assert.Len(t, entries, 1)
				// If the digest should already exist check that we actually hit it
				if tt.digestAlreadyExists {
					assert.Contains(t, buf.String(), "image already unpacked")
					assert.Equal(t, rs.UnpackTime, unpackDirStat.ModTime().Truncate(time.Second))
				} else if tt.oldDigestExists {
					assert.NotContains(t, buf.String(), "image already unpacked")
					assert.NotEqual(t, rs.UnpackTime, oldDigestModTime)
					assert.NoDirExists(t, oldDigestDir)
				} else {
					require.NotNil(t, rs.UnpackTime)
					require.NotNil(t, rs.ResolvedSource.Image)
					assert.False(t, rs.UnpackTime.IsZero())
				}
			} else {
				assert.Error(t, err)
				isTerminal := errors.Is(err, reconcile.TerminalError(nil))
				assert.Equal(t, tt.terminal, isTerminal, "expected terminal %v, got %v", tt.terminal, isTerminal)
			}

			assert.NoError(t, imgReg.Cleanup(ctx, tt.catalog))
			assert.NoError(t, imgReg.Cleanup(ctx, tt.catalog), "cleanup should ignore missing files")
		})
	}
}

// TestImageRegistryMissingLabelConsistentFailure is a test
// case that specifically tests that multiple calls to the
// ImageRegistry.Unpack() method return an error and is meant
// to ensure coverage of the bug reported in
// https://github.com/operator-framework/catalogd/issues/206
func TestImageRegistryMissingLabelConsistentFailure(t *testing.T) {
	// Create context, temporary cache directory,
	// and image registry source
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	testCache := t.TempDir()
	imgReg := &source.ContainersImageRegistry{
		BaseCachePath: testCache,
		SourceContextFunc: func(logger logr.Logger) (*types.SystemContext, error) {
			return &types.SystemContext{}, nil
		},
	}

	// Start a new server running an image registry
	srv := httptest.NewServer(registry.New())
	defer srv.Close()

	// parse the server url so we can grab just the host
	url, err := url.Parse(srv.URL)
	require.NoError(t, err)

	imgName, err := name.ParseReference(fmt.Sprintf("%s/%s", url.Host, "test-image:test"))
	require.NoError(t, err)

	image, err := random.Image(20, 20)
	require.NoError(t, err)

	err = remote.Write(imgName, image)
	require.NoError(t, err)

	catalog := &catalogdv1.ClusterCatalog{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: catalogdv1.ClusterCatalogSpec{
			Source: catalogdv1.CatalogSource{
				Type: catalogdv1.SourceTypeImage,
				Image: &catalogdv1.ImageSource{
					Ref: imgName.Name(),
				},
			},
		},
	}

	for i := 0; i < 3; i++ {
		_, err = imgReg.Unpack(ctx, catalog)
		require.Error(t, err, "unpack run ", i)
	}
}
