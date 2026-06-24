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
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	catalogclient "github.com/operator-framework/operator-controller/internal/operator-controller/catalogmetadata/client"
	mockcatalogclient "github.com/operator-framework/operator-controller/internal/testutil/mock/catalogclient"
	mockhttputil "github.com/operator-framework/operator-controller/internal/testutil/mock/httputil"
)

func defaultCatalog() *ocv1.ClusterCatalog {
	return &ocv1.ClusterCatalog{
		ObjectMeta: metav1.ObjectMeta{Name: "catalog-1"},
		Status: ocv1.ClusterCatalogStatus{
			Conditions: []metav1.Condition{{Type: ocv1.TypeServing, Status: metav1.ConditionTrue}},
			ResolvedSource: &ocv1.ResolvedCatalogSource{Image: &ocv1.ResolvedImageSource{
				Ref: "fake/catalog@sha256:fakesha",
			}},
			URLs: &ocv1.ClusterCatalogURLs{
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
		name       string
		catalog    func() *ocv1.ClusterCatalog
		pkgName    string
		setupCache func(ctrl *gomock.Controller) catalogclient.Cache
		assert     func(*testing.T, *declcfg.DeclarativeConfig, error)
	}
	for _, tc := range []testCase{
		{
			name: "not served",
			catalog: func() *ocv1.ClusterCatalog {
				return &ocv1.ClusterCatalog{ObjectMeta: metav1.ObjectMeta{Name: "catalog-1"}}
			},
			assert: func(t *testing.T, dc *declcfg.DeclarativeConfig, err error) {
				assert.ErrorContains(t, err, `catalog "catalog-1" is not being served`)
			},
		},
		{
			name:    "served, cache returns error",
			catalog: defaultCatalog,
			setupCache: func(ctrl *gomock.Controller) catalogclient.Cache {
				cache := mockcatalogclient.NewMockCache(ctrl)
				cache.EXPECT().Get(gomock.Any(), gomock.Any()).Return(nil, errors.New("fetch error"))
				return cache
			},
			assert: func(t *testing.T, dc *declcfg.DeclarativeConfig, err error) {
				assert.ErrorContains(t, err, `error retrieving cache for catalog "catalog-1"`)
			},
		},
		{
			name:    "served, invalid package path",
			catalog: defaultCatalog,
			setupCache: func(ctrl *gomock.Controller) catalogclient.Cache {
				cache := mockcatalogclient.NewMockCache(ctrl)
				cache.EXPECT().Get(gomock.Any(), gomock.Any()).Return(testFS, nil)
				return cache
			},
			pkgName: "/",
			assert: func(t *testing.T, dc *declcfg.DeclarativeConfig, err error) {
				assert.ErrorContains(t, err, `error getting package "/"`)
			},
		},
		{
			name:    "served, package missing",
			catalog: defaultCatalog,
			pkgName: "pkg-missing",
			setupCache: func(ctrl *gomock.Controller) catalogclient.Cache {
				cache := mockcatalogclient.NewMockCache(ctrl)
				cache.EXPECT().Get(gomock.Any(), gomock.Any()).Return(testFS, nil)
				return cache
			},
			assert: func(t *testing.T, fbc *declcfg.DeclarativeConfig, err error) {
				require.NoError(t, err)
				assert.Equal(t, &declcfg.DeclarativeConfig{}, fbc)
			},
		},
		{
			name:    "served, invalid package present",
			catalog: defaultCatalog,
			pkgName: "invalid-pkg-present",
			setupCache: func(ctrl *gomock.Controller) catalogclient.Cache {
				cache := mockcatalogclient.NewMockCache(ctrl)
				cache.EXPECT().Get(gomock.Any(), gomock.Any()).Return(fstest.MapFS{
					"invalid-pkg-present/olm.package/invalid-pkg-present.json": &fstest.MapFile{Data: []byte(`{"schema": "olm.package","name": 12345}`)},
				}, nil)
				return cache
			},
			assert: func(t *testing.T, fbc *declcfg.DeclarativeConfig, err error) {
				require.ErrorContains(t, err, `error loading package "invalid-pkg-present"`)
				assert.Nil(t, fbc)
			},
		},
		{
			name:    "served, package present",
			catalog: defaultCatalog,
			pkgName: "pkg-present",
			setupCache: func(ctrl *gomock.Controller) catalogclient.Cache {
				cache := mockcatalogclient.NewMockCache(ctrl)
				cache.EXPECT().Get(gomock.Any(), gomock.Any()).Return(testFS, nil)
				return cache
			},
			assert: func(t *testing.T, fbc *declcfg.DeclarativeConfig, err error) {
				require.NoError(t, err)
				assert.Equal(t, &declcfg.DeclarativeConfig{Packages: []declcfg.Package{{Schema: declcfg.SchemaPackage, Name: "pkg-present"}}}, fbc)
			},
		},
		{
			name:    "cache unpopulated",
			catalog: defaultCatalog,
			pkgName: "pkg-present",
			setupCache: func(ctrl *gomock.Controller) catalogclient.Cache {
				cache := mockcatalogclient.NewMockCache(ctrl)
				cache.EXPECT().Get(gomock.Any(), gomock.Any()).Return(nil, nil)
				return cache
			},
			assert: func(t *testing.T, fbc *declcfg.DeclarativeConfig, err error) {
				assert.ErrorContains(t, err, `cache for catalog "catalog-1" not found`)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			ctrl := gomock.NewController(t)

			var cache catalogclient.Cache
			if tc.setupCache != nil {
				cache = tc.setupCache(ctrl)
			}

			mockTripper := mockhttputil.NewMockRoundTripper(ctrl)
			c := catalogclient.New(cache, func() (*http.Client, error) {
				return &http.Client{
					// This is to prevent actual network calls
					Transport: mockTripper,
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
		name       string
		catalog    func() *ocv1.ClusterCatalog
		setupMocks func(t *testing.T, ctrl *gomock.Controller) (catalogclient.Cache, func() (*http.Client, error))
		assert     func(t *testing.T, fs fs.FS, err error)
	}
	for _, tt := range []testCase{
		{
			name:    "cache unpopulated, successful http request",
			catalog: defaultCatalog,
			setupMocks: func(t *testing.T, ctrl *gomock.Controller) (catalogclient.Cache, func() (*http.Client, error)) {
				cache := mockcatalogclient.NewMockCache(ctrl)
				cache.EXPECT().Put(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(catalogName, resolvedRef string, source io.Reader, errToCache error) (fs.FS, error) {
						buf := new(strings.Builder)
						if source != nil {
							_, _ = io.Copy(buf, source)
						}
						assert.Equal(t, "fake-success-response-body", buf.String())
						assert.NoError(t, errToCache)
						return testFS, errToCache
					},
				)

				mockTripper := mockhttputil.NewMockRoundTripper(ctrl)
				mockTripper.EXPECT().RoundTrip(gomock.Any()).Return(&http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("fake-success-response-body")),
				}, nil)

				httpClient := func() (*http.Client, error) {
					return &http.Client{Transport: mockTripper}, nil
				}
				return cache, httpClient
			},
			assert: func(t *testing.T, fs fs.FS, err error) {
				require.NoError(t, err)
				assert.Equal(t, testFS, fs)
			},
		},
		{
			name: "not served",
			catalog: func() *ocv1.ClusterCatalog {
				return &ocv1.ClusterCatalog{ObjectMeta: metav1.ObjectMeta{Name: "catalog-1"}}
			},
			setupMocks: func(t *testing.T, ctrl *gomock.Controller) (catalogclient.Cache, func() (*http.Client, error)) {
				cache := mockcatalogclient.NewMockCache(ctrl)
				return cache, nil
			},
			assert: func(t *testing.T, fs fs.FS, err error) {
				assert.Nil(t, fs)
				assert.ErrorContains(t, err, `catalog "catalog-1" is not being served`)
			},
		},
		{
			name:    "cache unpopulated, error on getting a http client",
			catalog: defaultCatalog,
			setupMocks: func(t *testing.T, ctrl *gomock.Controller) (catalogclient.Cache, func() (*http.Client, error)) {
				cache := mockcatalogclient.NewMockCache(ctrl)
				cache.EXPECT().Put(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(catalogName, resolvedRef string, source io.Reader, errToCache error) (fs.FS, error) {
						assert.Nil(t, source)
						assert.Error(t, errToCache)
						return nil, errToCache
					},
				)

				httpClient := func() (*http.Client, error) {
					return nil, errors.New("fake error getting a http client")
				}
				return cache, httpClient
			},
			assert: func(t *testing.T, fs fs.FS, err error) {
				assert.Nil(t, fs)
				assert.ErrorContains(t, err, "error getting HTTP client")
			},
		},
		{
			name:    "cache unpopulated, error on http request",
			catalog: defaultCatalog,
			setupMocks: func(t *testing.T, ctrl *gomock.Controller) (catalogclient.Cache, func() (*http.Client, error)) {
				cache := mockcatalogclient.NewMockCache(ctrl)
				cache.EXPECT().Put(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(catalogName, resolvedRef string, source io.Reader, errToCache error) (fs.FS, error) {
						assert.Nil(t, source)
						assert.Error(t, errToCache)
						return nil, errToCache
					},
				)

				mockTripper := mockhttputil.NewMockRoundTripper(ctrl)
				mockTripper.EXPECT().RoundTrip(gomock.Any()).Return(nil, errors.New("fake error on a http request"))

				httpClient := func() (*http.Client, error) {
					return &http.Client{Transport: mockTripper}, nil
				}
				return cache, httpClient
			},
			assert: func(t *testing.T, fs fs.FS, err error) {
				assert.Nil(t, fs)
				assert.ErrorContains(t, err, "error performing request")
			},
		},
		{
			name:    "cache unpopulated, unexpected http status",
			catalog: defaultCatalog,
			setupMocks: func(t *testing.T, ctrl *gomock.Controller) (catalogclient.Cache, func() (*http.Client, error)) {
				cache := mockcatalogclient.NewMockCache(ctrl)

				mockTripper := mockhttputil.NewMockRoundTripper(ctrl)
				mockTripper.EXPECT().RoundTrip(gomock.Any()).Return(&http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader("fake-unexpected-code-response-body")),
				}, nil)

				httpClient := func() (*http.Client, error) {
					return &http.Client{Transport: mockTripper}, nil
				}
				return cache, httpClient
			},
			assert: func(t *testing.T, fs fs.FS, err error) {
				assert.Nil(t, fs)
				assert.ErrorContains(t, err, "received unexpected response status code 500")
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ctrl := gomock.NewController(t)

			cache, httpClient := tt.setupMocks(t, ctrl)
			c := catalogclient.New(cache, httpClient)
			fs, err := c.PopulateCache(ctx, tt.catalog())
			tt.assert(t, fs, err)
		})
	}
}
