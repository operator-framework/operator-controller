package cache_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"

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

func TestCache(t *testing.T) {
	t.Run("FetchCatalogContents", func(t *testing.T) {
		type test struct {
			name           string
			catalog        *catalogd.Catalog
			contents       []byte
			wantErr        bool
			tripper        *MockTripper
			testCaching    bool
			shouldHitCache bool
		}
		for _, tt := range []test{
			{
				name: "valid non-cached fetch",
				catalog: &catalogd.Catalog{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-catalog",
					},
					Status: catalogd.CatalogStatus{
						ResolvedSource: &catalogd.ResolvedCatalogSource{
							Type: catalogd.SourceTypeImage,
							Image: &catalogd.ResolvedImageSource{
								ResolvedRef: "fake/catalog@sha256:fakesha",
							},
						},
					},
				},
				contents: []byte(strings.Join([]string{package1, bundle1, stableChannel}, "\n")),
				tripper:  &MockTripper{},
			},
			{
				name: "valid cached fetch",
				catalog: &catalogd.Catalog{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-catalog",
					},
					Status: catalogd.CatalogStatus{
						ResolvedSource: &catalogd.ResolvedCatalogSource{
							Type: catalogd.SourceTypeImage,
							Image: &catalogd.ResolvedImageSource{
								ResolvedRef: "fake/catalog@sha256:fakesha",
							},
						},
					},
				},
				contents:       []byte(strings.Join([]string{package1, bundle1, stableChannel}, "\n")),
				tripper:        &MockTripper{},
				testCaching:    true,
				shouldHitCache: true,
			},
			{
				name: "cached update fetch with changes",
				catalog: &catalogd.Catalog{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-catalog",
					},
					Status: catalogd.CatalogStatus{
						ResolvedSource: &catalogd.ResolvedCatalogSource{
							Type: catalogd.SourceTypeImage,
							Image: &catalogd.ResolvedImageSource{
								ResolvedRef: "fake/catalog@sha256:fakesha",
							},
						},
					},
				},
				contents:       []byte(strings.Join([]string{package1, bundle1, stableChannel}, "\n")),
				tripper:        &MockTripper{},
				testCaching:    true,
				shouldHitCache: false,
			},
			{
				name: "fetch error",
				catalog: &catalogd.Catalog{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-catalog",
					},
					Status: catalogd.CatalogStatus{
						ResolvedSource: &catalogd.ResolvedCatalogSource{
							Type: catalogd.SourceTypeImage,
							Image: &catalogd.ResolvedImageSource{
								ResolvedRef: "fake/catalog@sha256:fakesha",
							},
						},
					},
				},
				contents: []byte(strings.Join([]string{package1, bundle1, stableChannel}, "\n")),
				tripper:  &MockTripper{shouldError: true},
				wantErr:  true,
			},
			{
				name: "fetch internal server error response",
				catalog: &catalogd.Catalog{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-catalog",
					},
					Status: catalogd.CatalogStatus{
						ResolvedSource: &catalogd.ResolvedCatalogSource{
							Type: catalogd.SourceTypeImage,
							Image: &catalogd.ResolvedImageSource{
								ResolvedRef: "fake/catalog@sha256:fakesha",
							},
						},
					},
				},
				contents: []byte(strings.Join([]string{package1, bundle1, stableChannel}, "\n")),
				tripper:  &MockTripper{serverError: true},
				wantErr:  true,
			},
			{
				name:     "nil catalog",
				catalog:  nil,
				contents: []byte(strings.Join([]string{package1, bundle1, stableChannel}, "\n")),
				tripper:  &MockTripper{serverError: true},
				wantErr:  true,
			},
			{
				name: "nil catalog.status.resolvedSource",
				catalog: &catalogd.Catalog{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-catalog",
					},
					Status: catalogd.CatalogStatus{
						ResolvedSource: nil,
					},
				},
				contents: []byte(strings.Join([]string{package1, bundle1, stableChannel}, "\n")),
				tripper:  &MockTripper{serverError: true},
				wantErr:  true,
			},
			{
				name: "nil catalog.status.resolvedSource.image",
				catalog: &catalogd.Catalog{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-catalog",
					},
					Status: catalogd.CatalogStatus{
						ResolvedSource: &catalogd.ResolvedCatalogSource{
							Image: nil,
						},
					},
				},
				contents: []byte(strings.Join([]string{package1, bundle1, stableChannel}, "\n")),
				tripper:  &MockTripper{serverError: true},
				wantErr:  true,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				ctx := context.Background()
				cacheDir := t.TempDir()
				tt.tripper.content = tt.contents
				httpClient := http.DefaultClient
				httpClient.Transport = tt.tripper
				c := cache.NewFilesystemCache(cacheDir, httpClient)

				rc, err := c.FetchCatalogContents(ctx, tt.catalog)
				if !tt.wantErr {
					assert.NoError(t, err)
					filePath := filepath.Join(cacheDir, tt.catalog.Name, "data.json")
					assert.FileExists(t, filePath)
					fileContents, err := os.ReadFile(filePath)
					assert.NoError(t, err)
					assert.Equal(t, tt.contents, fileContents)

					data, err := io.ReadAll(rc)
					assert.NoError(t, err)
					assert.Equal(t, tt.contents, data)
					defer rc.Close()
				} else {
					assert.Error(t, err)
				}

				if tt.testCaching {
					if !tt.shouldHitCache {
						tt.catalog.Status.ResolvedSource.Image.ResolvedRef = "fake/catalog@sha256:shafake"
					}
					tt.tripper.content = append(tt.tripper.content, []byte(`{"schema": "olm.package", "name": "foobar"}`)...)
					rc, err := c.FetchCatalogContents(ctx, tt.catalog)
					assert.NoError(t, err)
					defer rc.Close()
					data, err := io.ReadAll(rc)
					assert.NoError(t, err)
					if !tt.shouldHitCache {
						assert.Equal(t, tt.tripper.content, data)
						assert.NotEqual(t, tt.contents, data)
					} else {
						assert.Equal(t, tt.contents, data)
					}
				}
			})
		}
	})
}

var _ http.RoundTripper = &MockTripper{}

type MockTripper struct {
	content     []byte
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

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(mt.content)),
	}, nil
}
