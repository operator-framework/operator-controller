package client_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"

	catalogClient "github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
)

func TestClientNew(t *testing.T) {
	testFS := fstest.MapFS{
		"pkg-present/olm.package/pkg-present.json": &fstest.MapFile{Data: []byte(`{"schema": "olm.package","name": "pkg-present"}`)},
	}

	type testCase struct {
		name    string
		catalog *catalogd.ClusterCatalog
		pkgName string
		fetcher catalogClient.Fetcher
		assert  func(*testing.T, *declcfg.DeclarativeConfig, error)
	}
	for _, tc := range []testCase{
		{
			name:    "not unpacked",
			catalog: &catalogd.ClusterCatalog{ObjectMeta: metav1.ObjectMeta{Name: "catalog-1"}},
			fetcher: fetcherFunc(func(context.Context, *catalogd.ClusterCatalog) (fs.FS, error) { return testFS, nil }),
			assert: func(t *testing.T, dc *declcfg.DeclarativeConfig, err error) {
				assert.ErrorContains(t, err, `catalog "catalog-1" is not being served`)
			},
		},
		{
			name: "unpacked, fetcher returns error",
			catalog: &catalogd.ClusterCatalog{
				Status: catalogd.ClusterCatalogStatus{Conditions: []metav1.Condition{{Type: catalogd.TypeServing, Status: metav1.ConditionTrue}}},
			},
			fetcher: fetcherFunc(func(context.Context, *catalogd.ClusterCatalog) (fs.FS, error) { return nil, errors.New("fetch error") }),
			assert: func(t *testing.T, dc *declcfg.DeclarativeConfig, err error) {
				assert.ErrorContains(t, err, `error fetching catalog contents: fetch error`)
			},
		},
		{
			name: "unpacked, invalid package path",
			catalog: &catalogd.ClusterCatalog{
				Status: catalogd.ClusterCatalogStatus{Conditions: []metav1.Condition{{Type: catalogd.TypeServing, Status: metav1.ConditionTrue}}},
			},
			fetcher: fetcherFunc(func(context.Context, *catalogd.ClusterCatalog) (fs.FS, error) { return testFS, nil }),
			pkgName: "/",
			assert: func(t *testing.T, dc *declcfg.DeclarativeConfig, err error) {
				assert.ErrorContains(t, err, `error getting package "/"`)
			},
		},
		{
			name: "unpacked, package missing",
			catalog: &catalogd.ClusterCatalog{
				Status: catalogd.ClusterCatalogStatus{Conditions: []metav1.Condition{{Type: catalogd.TypeServing, Status: metav1.ConditionTrue}}},
			},
			pkgName: "pkg-missing",
			fetcher: fetcherFunc(func(context.Context, *catalogd.ClusterCatalog) (fs.FS, error) { return testFS, nil }),
			assert: func(t *testing.T, fbc *declcfg.DeclarativeConfig, err error) {
				assert.NoError(t, err)
				assert.Equal(t, &declcfg.DeclarativeConfig{}, fbc)
			},
		},
		{
			name: "unpacked, invalid package present",
			catalog: &catalogd.ClusterCatalog{
				Status: catalogd.ClusterCatalogStatus{Conditions: []metav1.Condition{{Type: catalogd.TypeServing, Status: metav1.ConditionTrue}}},
			},
			pkgName: "invalid-pkg-present",
			fetcher: fetcherFunc(func(context.Context, *catalogd.ClusterCatalog) (fs.FS, error) {
				return fstest.MapFS{
					"invalid-pkg-present/olm.package/invalid-pkg-present.json": &fstest.MapFile{Data: []byte(`{"schema": "olm.package","name": 12345}`)},
				}, nil
			}),
			assert: func(t *testing.T, fbc *declcfg.DeclarativeConfig, err error) {
				assert.ErrorContains(t, err, `error loading package "invalid-pkg-present"`)
				assert.Nil(t, fbc)
			},
		},
		{
			name: "unpacked, package present",
			catalog: &catalogd.ClusterCatalog{
				Status: catalogd.ClusterCatalogStatus{Conditions: []metav1.Condition{{Type: catalogd.TypeServing, Status: metav1.ConditionTrue}}},
			},
			pkgName: "pkg-present",
			fetcher: fetcherFunc(func(context.Context, *catalogd.ClusterCatalog) (fs.FS, error) { return testFS, nil }),
			assert: func(t *testing.T, fbc *declcfg.DeclarativeConfig, err error) {
				assert.NoError(t, err)
				assert.Equal(t, &declcfg.DeclarativeConfig{Packages: []declcfg.Package{{Schema: declcfg.SchemaPackage, Name: "pkg-present"}}}, fbc)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := catalogClient.New(tc.fetcher)
			fbc, err := c.GetPackage(context.Background(), tc.catalog, tc.pkgName)
			tc.assert(t, fbc, err)
		})
	}
}

type fetcherFunc func(context.Context, *catalogd.ClusterCatalog) (fs.FS, error)

func (f fetcherFunc) FetchCatalogContents(ctx context.Context, catalog *catalogd.ClusterCatalog) (fs.FS, error) {
	return f(ctx, catalog)
}
