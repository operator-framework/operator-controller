package client_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	catalogd "github.com/operator-framework/operator-controller/api/catalogd/v1"
	catalogClient "github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/client"
)

func defaultCatalog() *catalogd.ClusterCatalog {
	return &catalogd.ClusterCatalog{
		ObjectMeta: metav1.ObjectMeta{Name: "catalog-1"},
		Status: catalogd.ClusterCatalogStatus{
			Conditions: []metav1.Condition{{Type: catalogd.TypeServing, Status: metav1.ConditionTrue}},
			ResolvedSource: &catalogd.ResolvedCatalogSource{Image: &catalogd.ResolvedImageSource{
				Ref: "fake/catalog@sha256:fakesha",
			}},
			URLs: &catalogd.ClusterCatalogURLs{
				Base: "https://fake-url.svc.local/catalogs/catalog-1",
			},
		},
	}
}

func TestClientGetPackage(t *testing.T) {
	testFS := fstest.MapFS{
		"pkg-present/olm.package/pkg-present.json": &fstest.MapFile{Data: []byte(`{"schema": "olm.package","name": "pkg-present"}`)},
	}

	type testCase struct {
		name    string
		catalog func() *catalogd.ClusterCatalog
		pkgName string
		cache   catalogClient.Cache
		assert  func(*testing.T, *declcfg.DeclarativeConfig, error)
	}
	for _, tc := range []testCase{
		{
			name: "not served",
			catalog: func() *catalogd.ClusterCatalog {
				return &catalogd.ClusterCatalog{ObjectMeta: metav1.ObjectMeta{Name: "catalog-1"}}
			},
			assert: func(t *testing.T, dc *declcfg.DeclarativeConfig, err error) {
				assert.ErrorContains(t, err, `catalog "catalog-1" is not being served`)
			},
		},
		{
			name:    "served, cache returns error",
			catalog: defaultCatalog,
			cache:   &fakeCache{getErr: errors.New("fetch error")},
			assert: func(t *testing.T, dc *declcfg.DeclarativeConfig, err error) {
				assert.ErrorContains(t, err, `error retrieving cache for catalog "catalog-1"`)
			},
		},
		{
			name:    "served, invalid package path",
			catalog: defaultCatalog,
			cache:   &fakeCache{getFS: testFS},
			pkgName: "/",
			assert: func(t *testing.T, dc *declcfg.DeclarativeConfig, err error) {
				assert.ErrorContains(t, err, `error getting package "/"`)
			},
		},
		{
			name:    "served, package missing",
			catalog: defaultCatalog,
			pkgName: "pkg-missing",
			cache:   &fakeCache{getFS: testFS},
			assert: func(t *testing.T, fbc *declcfg.DeclarativeConfig, err error) {
				require.NoError(t, err)
				assert.Equal(t, &declcfg.DeclarativeConfig{}, fbc)
			},
		},
		{
			name:    "served, invalid package present",
			catalog: defaultCatalog,
			pkgName: "invalid-pkg-present",
			cache: &fakeCache{getFS: fstest.MapFS{
				"invalid-pkg-present/olm.package/invalid-pkg-present.json": &fstest.MapFile{Data: []byte(`{"schema": "olm.package","name": 12345}`)},
			}},
			assert: func(t *testing.T, fbc *declcfg.DeclarativeConfig, err error) {
				require.ErrorContains(t, err, `error loading package "invalid-pkg-present"`)
				assert.Nil(t, fbc)
			},
		},
		{
			name:    "served, package present",
			catalog: defaultCatalog,
			pkgName: "pkg-present",
			cache:   &fakeCache{getFS: testFS},
			assert: func(t *testing.T, fbc *declcfg.DeclarativeConfig, err error) {
				require.NoError(t, err)
				assert.Equal(t, &declcfg.DeclarativeConfig{Packages: []declcfg.Package{{Schema: declcfg.SchemaPackage, Name: "pkg-present"}}}, fbc)
			},
		},
		{
			name:    "cache unpopulated",
			catalog: defaultCatalog,
			pkgName: "pkg-present",
			cache: &fakeCache{putFunc: func(source string, errToCache error) (fs.FS, error) {
				return testFS, nil
			}},
			assert: func(t *testing.T, fbc *declcfg.DeclarativeConfig, err error) {
				assert.ErrorContains(t, err, `cache for catalog "catalog-1" not found`)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			c := catalogClient.New(tc.cache, func() (*http.Client, error) {
				return &http.Client{
					// This is to prevent actual network calls
					Transport: &fakeTripper{resp: &http.Response{
						StatusCode: http.StatusOK,
						Body:       http.NoBody,
					}},
				}, nil
			})
			fbc, err := c.GetPackage(ctx, tc.catalog(), tc.pkgName)
			tc.assert(t, fbc, err)
		})
	}
}

func TestClientPopulateCache(t *testing.T) {
	testFS := fstest.MapFS{
		"pkg-present/olm.package/pkg-present.json": &fstest.MapFile{Data: []byte(`{"schema": "olm.package","name": "pkg-present"}`)},
	}

	type testCase struct {
		name               string
		catalog            func() *catalogd.ClusterCatalog
		httpClient         func() (*http.Client, error)
		putFuncConstructor func(t *testing.T) func(source string, errToCache error) (fs.FS, error)
		assert             func(t *testing.T, fs fs.FS, err error)
	}
	for _, tt := range []testCase{
		{
			name:    "cache unpopulated, successful http request",
			catalog: defaultCatalog,
			httpClient: func() (*http.Client, error) {
				return &http.Client{
					// This is to prevent actual network calls
					Transport: &fakeTripper{resp: &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader("fake-success-response-body")),
					}},
				}, nil
			},
			assert: func(t *testing.T, fs fs.FS, err error) {
				require.NoError(t, err)
				assert.Equal(t, testFS, fs)
			},
			putFuncConstructor: func(t *testing.T) func(source string, errToCache error) (fs.FS, error) {
				return func(source string, errToCache error) (fs.FS, error) {
					assert.Equal(t, "fake-success-response-body", source)
					assert.NoError(t, errToCache)
					return testFS, errToCache
				}
			},
		},
		{
			name: "not served",
			catalog: func() *catalogd.ClusterCatalog {
				return &catalogd.ClusterCatalog{ObjectMeta: metav1.ObjectMeta{Name: "catalog-1"}}
			},
			assert: func(t *testing.T, fs fs.FS, err error) {
				assert.Nil(t, fs)
				assert.ErrorContains(t, err, `catalog "catalog-1" is not being served`)
			},
		},
		{
			name:    "cache unpopulated, error on getting a http client",
			catalog: defaultCatalog,
			httpClient: func() (*http.Client, error) {
				return nil, errors.New("fake error getting a http client")
			},
			putFuncConstructor: func(t *testing.T) func(source string, errToCache error) (fs.FS, error) {
				return func(source string, errToCache error) (fs.FS, error) {
					assert.Empty(t, source)
					assert.Error(t, errToCache)
					return nil, errToCache
				}
			},
			assert: func(t *testing.T, fs fs.FS, err error) {
				assert.Nil(t, fs)
				assert.ErrorContains(t, err, "error getting HTTP client")
			},
		},
		{
			name:    "cache unpopulated, error on http request",
			catalog: defaultCatalog,
			httpClient: func() (*http.Client, error) {
				return &http.Client{
					// This is to prevent actual network calls
					Transport: &fakeTripper{err: errors.New("fake error on a http request")},
				}, nil
			},
			putFuncConstructor: func(t *testing.T) func(source string, errToCache error) (fs.FS, error) {
				return func(source string, errToCache error) (fs.FS, error) {
					assert.Empty(t, source)
					assert.Error(t, errToCache)
					return nil, errToCache
				}
			},
			assert: func(t *testing.T, fs fs.FS, err error) {
				assert.Nil(t, fs)
				assert.ErrorContains(t, err, "error performing request")
			},
		},
		{
			name:    "cache unpopulated, unexpected http status",
			catalog: defaultCatalog,
			httpClient: func() (*http.Client, error) {
				return &http.Client{
					// This is to prevent actual network calls
					Transport: &fakeTripper{resp: &http.Response{
						StatusCode: http.StatusInternalServerError,
						Body:       io.NopCloser(strings.NewReader("fake-unexpected-code-response-body")),
					}},
				}, nil
			},
			putFuncConstructor: func(t *testing.T) func(source string, errToCache error) (fs.FS, error) {
				return func(source string, errToCache error) (fs.FS, error) {
					assert.Empty(t, source)
					assert.Error(t, errToCache)
					return nil, errToCache
				}
			},
			assert: func(t *testing.T, fs fs.FS, err error) {
				assert.Nil(t, fs)
				assert.ErrorContains(t, err, "received unexpected response status code 500")
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			cache := &fakeCache{}
			if tt.putFuncConstructor != nil {
				cache.putFunc = tt.putFuncConstructor(t)
			}

			c := catalogClient.New(cache, tt.httpClient)
			fs, err := c.PopulateCache(ctx, tt.catalog())
			tt.assert(t, fs, err)
		})
	}
}

type fakeCache struct {
	getFS  fs.FS
	getErr error

	putFunc func(source string, errToCache error) (fs.FS, error)
}

func (c *fakeCache) Get(catalogName, resolvedRef string) (fs.FS, error) {
	return c.getFS, c.getErr
}

func (c *fakeCache) Put(catalogName, resolvedRef string, source io.Reader, errToCache error) (fs.FS, error) {
	if c.putFunc != nil {
		buf := new(strings.Builder)
		if source != nil {
			io.Copy(buf, source) // nolint:errcheck
		}
		return c.putFunc(buf.String(), errToCache)
	}

	return nil, errors.New("unexpected error")
}

type fakeTripper struct {
	resp *http.Response
	err  error
}

func (ft *fakeTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return ft.resp, ft.err
}
