package source_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	testFilePathBase string = ".image-registry-test"
	bogusDigestHex   string = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testImageTag     string = "test-tag"
	testImageName    string = "test-image"
	badImageName     string = "bad-image"
	testFileName     string = "test-file"
	testFileContents string = "test-content"
)

func newReference(host, repo, ref string) (name.Reference, error) {
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:%s", host, repo, ref), name.WeakValidation)
	if err == nil {
		return tag, nil
	}
	return name.NewDigest(fmt.Sprintf("%s/%s@%s", host, repo, ref), name.WeakValidation)
}

func imageToRawManifest(img v1.Image) ([]byte, error) {
	manifest, err := img.Manifest()
	if err != nil {
		return nil, err
	}

	layers, err := img.Layers()
	if err != nil {
		return nil, err
	}

	rc, err := layers[0].Compressed()
	if err != nil {
		return nil, err
	}

	lb, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	manifest.Layers[0].Data = lb
	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}

	return rawManifest, nil
}

// Adapted from: https://github.com/google/go-containerregistry/blob/main/pkg/v1/remote/image_test.go
// serveImageManifest starts a primitive image registry server hosting two images: "test-image" and "bad-image".
func serveImageManifest(rawManifest []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case fmt.Sprintf("/v2/%s/manifests/%s", testImageName, testImageTag):
			if r.Method == http.MethodHead {
				w.Header().Set("Content-Length", fmt.Sprint(len(rawManifest)))
				w.Header().Set("Docker-Content-Digest", fmt.Sprintf("sha256:%s", bogusDigestHex))
				w.WriteHeader(http.StatusOK)
			} else if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusBadRequest)
			}
			_, err := w.Write(rawManifest)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
		case fmt.Sprintf("/v2/%s/manifests/%s", badImageName, testImageTag):
		case fmt.Sprintf("/v2/%s/manifests/sha256:%s", badImageName, bogusDigestHex):
			if r.Method == http.MethodHead {
				w.Header().Set("Content-Length", fmt.Sprint(len(rawManifest)))
				w.Header().Set("Docker-Content-Digest", fmt.Sprintf("sha256:%s", bogusDigestHex))
				// We must set Content-Type since we're returning empty data below
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				return
			} else if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusBadRequest)
			}
			_, err := w.Write(make([]byte, 0))
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
}

func testFileCleanup() {
	if _, err := os.Stat(testFilePathBase); err != nil && errors.Is(err, os.ErrNotExist) {
		// Nothing to clean up
		return
	} else if err != nil {
		log.Fatalf("error occurred locating unpack folder in post-test cleanup: %v", err)
	}
	// Ensure permissions and remove the temporary directory
	err := os.Chmod(testFilePathBase, os.ModePerm)
	if err != nil {
		log.Fatalf("error occurred ensuring unpack folder permissions in post-test cleanup: %v", err)
	}
	err = os.RemoveAll(testFilePathBase)
	if err != nil {
		log.Fatalf("error occurred deleting unpack folder in post-test cleanup: %v", err)
	}
}

var HostedImageReference name.Reference

func TestMain(m *testing.M) {
	// Generate an image with file contents
	img, err := crane.Image(map[string][]byte{testFileName: []byte(testFileContents)})
	if err != nil {
		log.Fatalf("failed to generate image for test")
	}
	// Create a raw bytes manifest from the image
	rawManifest, err := imageToRawManifest(img)
	if err != nil {
		log.Fatalf("failed to generate manifest from image")
	}

	// Start the image registry and serve the generated image manifest
	server := serveImageManifest(rawManifest)
	if err != nil {
		log.Fatalf("image registry server failed to start")
	}
	u, err := url.Parse(server.URL)
	if err != nil {
		log.Fatalf("invalid server URL from image registry")
	}
	HostedImageReference, err = newReference(u.Host, testImageName, testImageTag)
	if err != nil {
		log.Fatalf("failed to generate image reference for served image")
	}
	code := m.Run()
	server.Close()
	os.Exit(code)
}

func TestUnpackValidInsecure(t *testing.T) {
	defer testFileCleanup()
	unpacker := &source.ImageRegistry{
		BaseCachePath: testFilePathBase,
	}
	bundleSource := &source.BundleSource{
		Type: source.SourceTypeImage,
		Image: &source.ImageSource{
			Ref:                   HostedImageReference.String(),
			InsecureSkipTLSVerify: true,
		},
	}

	unpackPath := filepath.Join(unpacker.BaseCachePath, bundleSource.Name, bogusDigestHex)

	// Create another folder to simulate an old unpacked bundle
	oldBundlePath := filepath.Join(unpacker.BaseCachePath, bundleSource.Name, "foo")
	require.NoError(t, os.MkdirAll(oldBundlePath, os.ModePerm))

	// Attempt to pull and unpack the image
	result, err := unpacker.Unpack(context.Background(), bundleSource)
	// Check Result
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, result.State, source.StateUnpacked)
	// Make sure the old bundle was cleaned up
	require.NoDirExists(t, oldBundlePath)

	// Give permissions to read the file
	require.NoError(t, os.Chmod(filepath.Join(unpackPath, testFileName), 0400))
	unpackedFile, err := os.ReadFile(filepath.Join(unpackPath, testFileName))
	require.NoError(t, err)
	// Ensure the unpacked file matches the source content
	require.Equal(t, []byte(testFileContents), unpackedFile)
}

func TestUnpackValidUsesCache(t *testing.T) {
	defer testFileCleanup()
	unpacker := &source.ImageRegistry{
		BaseCachePath: testFilePathBase,
	}
	bundleSource := &source.BundleSource{
		Type: source.SourceTypeImage,
		Image: &source.ImageSource{
			Ref:                   fmt.Sprintf("%s@sha256:%s", HostedImageReference.Context().Name(), bogusDigestHex),
			InsecureSkipTLSVerify: true,
		},
	}

	// Populate the bundle cache with a folder
	testCacheFilePath := filepath.Join(unpacker.BaseCachePath, bundleSource.Name, bogusDigestHex, "test-folder")
	require.NoError(t, os.MkdirAll(testCacheFilePath, os.ModePerm))

	// Attempt to pull and unpack the image
	result, err := unpacker.Unpack(context.Background(), bundleSource)
	// Check Result
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, result.State, source.StateUnpacked)
	// Make sure the old file was not cleaned up
	require.DirExists(t, testCacheFilePath)
}

func TestUnpackCacheCheckError(t *testing.T) {
	defer testFileCleanup()
	unpacker := &source.ImageRegistry{
		BaseCachePath: testFilePathBase,
	}
	bundleSource := &source.BundleSource{
		Type: source.SourceTypeImage,
		Image: &source.ImageSource{
			Ref:                   HostedImageReference.String(),
			InsecureSkipTLSVerify: true,
		},
	}

	// Create the unpack path and restrict its permissions
	unpackPath := filepath.Join(unpacker.BaseCachePath, bundleSource.Name, bogusDigestHex)
	require.NoError(t, os.MkdirAll(unpackPath, os.ModePerm))
	require.NoError(t, os.Chmod(testFilePathBase, 0000))
	// Attempt to pull and unpack the image
	_, err := unpacker.Unpack(context.Background(), bundleSource)
	// Check Result
	require.Error(t, err)
}

func TestUnpackUnservedImageReference(t *testing.T) {
	defer testFileCleanup()
	unpacker := &source.ImageRegistry{
		BaseCachePath: testFilePathBase,
	}
	bundleSource := &source.BundleSource{
		Type: source.SourceTypeImage,
		Image: &source.ImageSource{
			// Use a valid reference that is not served
			Ref:                   fmt.Sprintf("%s/%s:unserved-tag", HostedImageReference.Context().Registry.RegistryStr(), badImageName),
			InsecureSkipTLSVerify: true,
		},
	}

	// Attempt to pull and unpack the image
	_, err := unpacker.Unpack(context.Background(), bundleSource)
	// Check Result
	require.Error(t, err)
}

func TestUnpackFailure(t *testing.T) {
	defer testFileCleanup()
	unpacker := &source.ImageRegistry{
		BaseCachePath: testFilePathBase,
	}
	bundleSource := &source.BundleSource{
		Type: source.SourceTypeImage,
		Image: &source.ImageSource{
			// Use a valid reference that is served but will return bad image content
			Ref:                   fmt.Sprintf("%s/%s:%s", HostedImageReference.Context().Registry.RegistryStr(), badImageName, testImageTag),
			InsecureSkipTLSVerify: true,
		},
	}

	// Attempt to pull and unpack the image
	_, err := unpacker.Unpack(context.Background(), bundleSource)
	// Check Result
	require.Error(t, err)
}

func TestUnpackFailureDigest(t *testing.T) {
	defer testFileCleanup()
	unpacker := &source.ImageRegistry{
		BaseCachePath: testFilePathBase,
	}
	bundleSource := &source.BundleSource{
		Type: source.SourceTypeImage,
		Image: &source.ImageSource{
			// Use a valid reference that is served but will return bad image content
			Ref:                   fmt.Sprintf("%s/%s@sha256:%s", HostedImageReference.Context().Registry.RegistryStr(), badImageName, bogusDigestHex),
			InsecureSkipTLSVerify: true,
		},
	}

	// Attempt to pull and unpack the image
	_, err := unpacker.Unpack(context.Background(), bundleSource)
	// Check Result
	require.Error(t, err)
	// Unpacker gives an error of type Unrecoverable
	require.IsType(t, &source.Unrecoverable{}, err)
}

func TestUnpackInvalidSourceType(t *testing.T) {
	unpacker := &source.ImageRegistry{}
	// Create BundleSource with invalid source type
	bundleSource := &source.BundleSource{
		Type: "invalid",
	}

	shouldPanic := func() {
		// Attempt to pull and unpack the image
		_, err := unpacker.Unpack(context.Background(), bundleSource)
		if err != nil {
			t.Error("func should have panicked")
		}
	}
	require.Panics(t, shouldPanic)
}

func TestUnpackInvalidNilImage(t *testing.T) {
	unpacker := &source.ImageRegistry{}
	// Create BundleSource with nil Image
	bundleSource := &source.BundleSource{
		Type:  source.SourceTypeImage,
		Image: nil,
	}

	// Attempt to unpack
	result, err := unpacker.Unpack(context.Background(), bundleSource)
	require.Error(t, err)
	require.NoDirExists(t, testFilePathBase)
	assert.Nil(t, result)
}

func TestUnpackInvalidImageRef(t *testing.T) {
	unpacker := &source.ImageRegistry{}
	// Create BundleSource with malformed image reference
	bundleSource := &source.BundleSource{
		Type: source.SourceTypeImage,
		Image: &source.ImageSource{
			Ref: "invalid image ref",
		},
	}

	// Attempt to unpack
	result, err := unpacker.Unpack(context.Background(), bundleSource)
	require.Error(t, err)
	require.NoDirExists(t, testFilePathBase)
	assert.Nil(t, result)
}
