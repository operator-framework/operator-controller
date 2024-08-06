package source_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/internal/rukpak/source"
)

const (
	testFilePathBase   string = ".image-registry-test"
	bogusDigestHex     string = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testImageTag       string = "test-tag"
	testImageName      string = "test"
	manifestSourcePath string = "../../../testdata/bundles/registry-v1/prometheus-operator.v1.0.0/manifests"
)

func newReference(host, repo, ref string) (name.Reference, error) {
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:%s", host, repo, ref), name.WeakValidation)
	if err == nil {
		return tag, nil
	}
	return name.NewDigest(fmt.Sprintf("%s/%s@%s", host, repo, ref), name.WeakValidation)
}

func manifestsToImage(t *testing.T, folderPath string) (v1.Image, error) {
	dirs, err := os.ReadDir(folderPath)
	require.NoError(t, err)
	filemap := make(map[string][]byte)
	for _, file := range dirs {
		if file.IsDir() {
			continue
		}
		f, err := os.ReadFile(fmt.Sprintf("%s/%s", folderPath, file.Name()))
		require.NoError(t, err)
		filemap[file.Name()] = f
	}
	return crane.Image(filemap)
}

// Adapted from: https://github.com/google/go-containerregistry/blob/main/pkg/v1/remote/image_test.go
func serveImage(t *testing.T, img v1.Image) *httptest.Server {
	manifest, err := img.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	layers, err := img.Layers()
	if err != nil {
		t.Fatal(err)
	}

	rc, err := layers[0].Compressed()
	if err != nil {
		t.Fatal(err)
	}
	lb, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}

	manifest.Layers[0].Data = lb
	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case fmt.Sprintf("/v2/test/manifests/%s", testImageTag):
			if r.Method == http.MethodHead {
				w.Header().Set("Content-Length", fmt.Sprint(len(rawManifest)))
				w.Header().Set("Docker-Content-Digest", fmt.Sprintf("sha256:%s", bogusDigestHex))
				w.WriteHeader(http.StatusOK)
			} else if r.Method != http.MethodGet {
				t.Errorf("Method; got %v, want %v", r.Method, http.MethodGet)
			}
			_, err := w.Write(rawManifest)
			if err != nil {
				t.Errorf("failed to write manifest: %s", err.Error())
			}
		default:
			//explode if we try to read blob or config
			t.Fatalf("Unexpected path: %v", r.URL.Path)
		}
	}))
}

func TestUnpack(t *testing.T) {
	img, err := manifestsToImage(t, manifestSourcePath)
	if err != nil {
		t.Fatalf("failed to generate image: %v", err)
	}
	defer func() {
		// Remove the temporary directory
		err = os.RemoveAll(testFilePathBase)
		require.NoErrorf(t, err, "error occurred in post-test cleanup")
	}()

	tests := []struct {
		name         string
		unpacker     *source.ImageRegistry
		bundleSource *source.BundleSource
		wantErr      bool
		wantPanic    bool
	}{
		{
			name:    "happy path",
			wantErr: false,
			unpacker: &source.ImageRegistry{
				BaseCachePath: filepath.Join(testFilePathBase, "unpack"),
			},
			bundleSource: &source.BundleSource{
				Name: "prometheus",
				Type: source.SourceTypeImage,
				Image: &source.ImageSource{
					// Set Ref in test body
					InsecureSkipTLSVerify: true,
				},
			},
		},
		{
			name:      "invalid bundle source type",
			wantPanic: true,
			unpacker: &source.ImageRegistry{
				BaseCachePath: filepath.Join(testFilePathBase, "unpack"),
			},
			bundleSource: &source.BundleSource{
				Name: "prometheus",
				Type: "Invalid",
				Image: &source.ImageSource{
					// Set Ref in test body
					InsecureSkipTLSVerify: true,
				},
			},
		},
		{
			name:    "nil image",
			wantErr: true,
			unpacker: &source.ImageRegistry{
				BaseCachePath: filepath.Join(testFilePathBase, "unpack"),
			},
			bundleSource: &source.BundleSource{
				Name:  "prometheus",
				Type:  source.SourceTypeImage,
				Image: nil,
			},
		},
		{
			name:    "invalid image ref",
			wantErr: true,
			unpacker: &source.ImageRegistry{
				BaseCachePath: filepath.Join(testFilePathBase, "unpack"),
			},
			bundleSource: &source.BundleSource{
				Name: "prometheus",
				Type: source.SourceTypeImage,
				Image: &source.ImageSource{
					Ref: "invalid image ref",
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Start the image registry and serve the provided image
			server := serveImage(t, img)
			defer server.Close()

			// Get the image reference for the image we're serving
			u, err := url.Parse(server.URL)
			if err != nil {
				t.Fatalf("url.Parse(%v) = %v", server.URL, err)
			}
			ref, err := newReference(u.Host, testImageName, testImageTag)
			if err != nil {
				t.Fatalf("newReference err: %s", err.Error())
			}

			// Set our image reference
			if tc.bundleSource.Image != nil && tc.bundleSource.Image.Ref == "" {
				tc.bundleSource.Image.Ref = ref.String()
			}

			// Make sure we clean up any unpacked files
			unpackPath := filepath.Join(tc.unpacker.BaseCachePath, tc.bundleSource.Name, bogusDigestHex)
			defer func() {
				if _, err := os.ReadDir(testFilePathBase); err != nil {
					if !os.IsNotExist(err) {
						t.Fatalf("error locating test unpack location: %s", err)
					}
					// No dir to clean up
					return
				}
				// Run the unpacker Cleanup
				err = tc.unpacker.Cleanup(context.Background(), tc.bundleSource)
				require.NoErrorf(t, err, "unexpected error occurred in unpacker cleanup")
				// Make sure the unpacked files are gone
				require.NoDirExists(t, unpackPath)
			}()

			// Recover from any expected panics
			if tc.wantPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("expected panic did not occur")
					}
				}()
			}

			// Attempt to pull and unpack the image
			result, err := tc.unpacker.Unpack(context.Background(), tc.bundleSource)
			if tc.wantErr {
				require.Error(t, err)
				require.NoDirExists(t, unpackPath)
				assert.Nil(t, result)
				return
			}

			// Check Result
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, result.State, source.StateUnpacked)

			// Check unpack directory
			require.DirExists(t, unpackPath)
			dirs, err := os.ReadDir(unpackPath)
			require.NoError(t, err)
			unpackedFilemap := map[string][]byte{}
			for _, file := range dirs {
				// Give read permissions for unpacked files
				require.NoError(t, os.Chmod(fmt.Sprintf("%s/%s", unpackPath, file.Name()), 0400))
				f, err := os.ReadFile(fmt.Sprintf("%s/%s", unpackPath, file.Name()))
				require.NoError(t, err)
				unpackedFilemap[file.Name()] = f
			}

			// Check files in the unpack directory
			sourceFiles, err := os.ReadDir(manifestSourcePath)
			require.NoError(t, err)
			require.Equalf(t, len(unpackedFilemap), len(sourceFiles), "manifest source directory and unpacked result do not contain the same number of files")
			for _, sourceFile := range sourceFiles {
				expectedFileBuff, err := os.ReadFile(fmt.Sprintf("%s/%s", manifestSourcePath, sourceFile.Name()))
				require.NoError(t, err)
				// Files should be equal across both directories
				if unpackedFile, ok := unpackedFilemap[sourceFile.Name()]; !ok {
					t.Fatalf("unpacked directory missing file from source directory: %s", unpackedFile)
				}
				require.Equalf(t, expectedFileBuff, unpackedFilemap[sourceFile.Name()], "unpacked file %s differs from source file", sourceFile.Name())
			}
		})
	}
}
