package cache_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata/cache"
)

const (
	package1 = `{
		"schema": "olm.package",
		"name": "fake1"
	}`

	bundle1 = `{
		"schema": "olm.bundle",
		"name": "fake1.v1.0.0",
		"package": "fake1",
		"image": "fake-image",
		"properties": [
			{
				"type": "olm.package",
				"value": {"packageName":"fake1","version":"1.0.0"}
			}
		]
	}`

	stableChannel = `{
		"schema": "olm.channel",
		"name": "stable",
		"package": "fake1",
		"entries": [
			{
				"name": "fake1.v1.0.0"
			}
		]
	}`
)

var defaultFS = fstest.MapFS{
	"fake1/olm.package/fake1.json":       &fstest.MapFile{Data: []byte(package1)},
	"fake1/olm.bundle/fake1.v1.0.0.json": &fstest.MapFile{Data: []byte(bundle1)},
	"fake1/olm.channel/stable.json":      &fstest.MapFile{Data: []byte(stableChannel)},
}

func TestFilesystemCache(t *testing.T) {
	type test struct {
		name           string
		catalog        *catalogd.ClusterCatalog
		contents       fstest.MapFS
		wantErr        bool
		tripper        *MockTripper
		testCaching    bool
		shouldHitCache bool
	}
	for _, tt := range []test{
		{
			name: "valid non-cached fetch",
			catalog: &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-catalog",
				},
				Status: catalogd.ClusterCatalogStatus{
					ResolvedSource: &catalogd.ResolvedCatalogSource{
						Type: catalogd.SourceTypeImage,
						Image: &catalogd.ResolvedImageSource{
							ResolvedRef: "fake/catalog@sha256:fakesha",
						},
					},
				},
			},
			contents: defaultFS,
			tripper:  &MockTripper{},
		},
		{
			name: "valid cached fetch",
			catalog: &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-catalog",
				},
				Status: catalogd.ClusterCatalogStatus{
					ResolvedSource: &catalogd.ResolvedCatalogSource{
						Type: catalogd.SourceTypeImage,
						Image: &catalogd.ResolvedImageSource{
							ResolvedRef: "fake/catalog@sha256:fakesha",
						},
					},
				},
			},
			contents:       defaultFS,
			tripper:        &MockTripper{},
			testCaching:    true,
			shouldHitCache: true,
		},
		{
			name: "cached update fetch with changes",
			catalog: &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-catalog",
				},
				Status: catalogd.ClusterCatalogStatus{
					ResolvedSource: &catalogd.ResolvedCatalogSource{
						Type: catalogd.SourceTypeImage,
						Image: &catalogd.ResolvedImageSource{
							ResolvedRef: "fake/catalog@sha256:fakesha",
						},
					},
				},
			},
			contents:       defaultFS,
			tripper:        &MockTripper{},
			testCaching:    true,
			shouldHitCache: false,
		},
		{
			name: "fetch error",
			catalog: &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-catalog",
				},
				Status: catalogd.ClusterCatalogStatus{
					ResolvedSource: &catalogd.ResolvedCatalogSource{
						Type: catalogd.SourceTypeImage,
						Image: &catalogd.ResolvedImageSource{
							ResolvedRef: "fake/catalog@sha256:fakesha",
						},
					},
				},
			},
			contents: defaultFS,
			tripper:  &MockTripper{shouldError: true},
			wantErr:  true,
		},
		{
			name: "fetch internal server error response",
			catalog: &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-catalog",
				},
				Status: catalogd.ClusterCatalogStatus{
					ResolvedSource: &catalogd.ResolvedCatalogSource{
						Type: catalogd.SourceTypeImage,
						Image: &catalogd.ResolvedImageSource{
							ResolvedRef: "fake/catalog@sha256:fakesha",
						},
					},
				},
			},
			contents: defaultFS,
			tripper:  &MockTripper{serverError: true},
			wantErr:  true,
		},
		{
			name:     "nil catalog",
			catalog:  nil,
			contents: defaultFS,
			tripper:  &MockTripper{serverError: true},
			wantErr:  true,
		},
		{
			name: "nil catalog.status.resolvedSource",
			catalog: &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-catalog",
				},
				Status: catalogd.ClusterCatalogStatus{
					ResolvedSource: nil,
				},
			},
			contents: defaultFS,
			tripper:  &MockTripper{serverError: true},
			wantErr:  true,
		},
		{
			name: "nil catalog.status.resolvedSource.image",
			catalog: &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-catalog",
				},
				Status: catalogd.ClusterCatalogStatus{
					ResolvedSource: &catalogd.ResolvedCatalogSource{
						Image: nil,
					},
				},
			},
			contents: defaultFS,
			tripper:  &MockTripper{serverError: true},
			wantErr:  true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cacheDir := t.TempDir()
			tt.tripper.content = make(fstest.MapFS)
			maps.Copy(tt.tripper.content, tt.contents)
			httpClient := http.DefaultClient
			httpClient.Transport = tt.tripper
			c := cache.NewFilesystemCache(cacheDir, httpClient)

			actualFS, err := c.FetchCatalogContents(ctx, tt.catalog)
			if !tt.wantErr {
				assert.NoError(t, err)
				assert.NoError(t, equalFilesystems(tt.contents, actualFS))
			} else {
				assert.Error(t, err)
			}

			if tt.testCaching {
				if !tt.shouldHitCache {
					tt.catalog.Status.ResolvedSource.Image.ResolvedRef = "fake/catalog@sha256:shafake"
				}
				tt.tripper.content["foobar/olm.package/foobar.json"] = &fstest.MapFile{Data: []byte(`{"schema": "olm.package", "name": "foobar"}`)}
				actualFS, err := c.FetchCatalogContents(ctx, tt.catalog)
				assert.NoError(t, err)
				if !tt.shouldHitCache {
					assert.NoError(t, equalFilesystems(tt.tripper.content, actualFS))
					assert.ErrorContains(t, equalFilesystems(tt.contents, actualFS), "foobar/olm.package/foobar.json")
				} else {
					assert.NoError(t, equalFilesystems(tt.contents, actualFS))
				}
			}
		})
	}
}

var _ http.RoundTripper = &MockTripper{}

type MockTripper struct {
	content     fstest.MapFS
	shouldError bool
	serverError bool
}

func (mt *MockTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	if mt.shouldError {
		return nil, errors.New("mock tripper error")
	}

	if mt.serverError {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       http.NoBody,
		}, nil
	}

	pr, pw := io.Pipe()

	go func() {
		_ = pw.CloseWithError(declcfg.WalkMetasFS(context.Background(), mt.content, func(_ string, meta *declcfg.Meta, err error) error {
			if err != nil {
				return err
			}
			_, err = pw.Write(meta.Blob)
			return err
		}))
	}()

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       pr,
	}, nil
}

func equalFilesystems(expected, actual fs.FS) error {
	normalizeJSON := func(data []byte) []byte {
		var v interface{}
		if err := json.Unmarshal(data, &v); err != nil {
			return data
		}
		norm, err := json.Marshal(v)
		if err != nil {
			return data
		}
		return norm
	}
	compare := func(expected, actual fs.FS, path string) error {
		expectedData, expectedErr := fs.ReadFile(expected, path)
		actualData, actualErr := fs.ReadFile(actual, path)

		switch {
		case expectedErr == nil && actualErr != nil:
			return fmt.Errorf("path %q: read error in actual FS: %v", path, actualErr)
		case expectedErr != nil && actualErr == nil:
			return fmt.Errorf("path %q: read error in expected FS: %v", path, expectedErr)
		case expectedErr != nil && actualErr != nil && expectedErr.Error() != actualErr.Error():
			return fmt.Errorf("path %q: different read errors: expected: %v, actual: %v", path, expectedErr, actualErr)
		}

		if filepath.Ext(path) == ".json" {
			expectedData = normalizeJSON(expectedData)
			actualData = normalizeJSON(actualData)
		}

		if !bytes.Equal(expectedData, actualData) {
			return fmt.Errorf("path %q: file contents do not match: %s", path, cmp.Diff(string(expectedData), string(actualData)))
		}
		return nil
	}

	paths := sets.New[string]()
	for _, fsys := range []fs.FS{expected, actual} {
		if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			paths.Insert(path)
			return nil
		}); err != nil {
			return err
		}
	}

	var cmpErrs []error
	for _, path := range sets.List(paths) {
		if err := compare(expected, actual, path); err != nil {
			cmpErrs = append(cmpErrs, err)
		}
	}
	return errors.Join(cmpErrs...)
}
