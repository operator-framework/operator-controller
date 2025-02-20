package controllers_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	catalogd "github.com/operator-framework/operator-controller/api/catalogd/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/scheme"
)

func TestClusterCatalogReconcilerFinalizers(t *testing.T) {
	const fakeResolvedRef = "fake/catalog@sha256:fakesha1"
	catalogKey := types.NamespacedName{Name: "test-catalog"}

	for _, tt := range []struct {
		name                    string
		catalog                 *catalogd.ClusterCatalog
		catalogCache            mockCatalogCache
		catalogCachePopulator   mockCatalogCachePopulator
		wantGetCacheCalled      bool
		wantRemoveCacheCalled   bool
		wantPopulateCacheCalled bool
		wantErr                 string
	}{
		{
			name: "catalog exists - cache unpopulated",
			catalog: &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: catalogKey.Name,
				},
				Status: catalogd.ClusterCatalogStatus{
					ResolvedSource: &catalogd.ResolvedCatalogSource{
						Image: &catalogd.ResolvedImageSource{
							Ref: fakeResolvedRef,
						},
					},
				},
			},
			catalogCachePopulator: mockCatalogCachePopulator{
				populateCacheFunc: func(ctx context.Context, catalog *catalogd.ClusterCatalog) (fs.FS, error) {
					assert.Equal(t, catalogKey.Name, catalog.Name)
					return nil, nil
				},
			},
			wantGetCacheCalled:      true,
			wantPopulateCacheCalled: true,
		},
		{
			name: "catalog exists - cache already populated",
			catalog: &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: catalogKey.Name,
				},
				Status: catalogd.ClusterCatalogStatus{
					ResolvedSource: &catalogd.ResolvedCatalogSource{
						Image: &catalogd.ResolvedImageSource{
							Ref: fakeResolvedRef,
						},
					},
				},
			},
			catalogCache: mockCatalogCache{
				getFunc: func(catalogName, resolvedRef string) (fs.FS, error) {
					assert.Equal(t, catalogKey.Name, catalogName)
					assert.Equal(t, fakeResolvedRef, resolvedRef)
					// Just any non-nil fs.FS to simulate existence of cache
					return fstest.MapFS{}, nil
				},
			},
			wantGetCacheCalled: true,
		},
		{
			name: "catalog exists - catalog not yet resolved",
			catalog: &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: catalogKey.Name,
				},
			},
		},
		{
			name: "catalog exists - error on cache population",
			catalog: &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: catalogKey.Name,
				},
				Status: catalogd.ClusterCatalogStatus{
					ResolvedSource: &catalogd.ResolvedCatalogSource{
						Image: &catalogd.ResolvedImageSource{
							Ref: fakeResolvedRef,
						},
					},
				},
			},
			catalogCachePopulator: mockCatalogCachePopulator{
				populateCacheFunc: func(ctx context.Context, catalog *catalogd.ClusterCatalog) (fs.FS, error) {
					assert.Equal(t, catalogKey.Name, catalog.Name)
					return nil, errors.New("fake error from populate cache function")
				},
			},
			wantGetCacheCalled:      true,
			wantPopulateCacheCalled: true,
			wantErr:                 "error populating cache for catalog",
		},
		{
			name: "catalog exists - error on cache get",
			catalog: &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: catalogKey.Name,
				},
				Status: catalogd.ClusterCatalogStatus{
					ResolvedSource: &catalogd.ResolvedCatalogSource{
						Image: &catalogd.ResolvedImageSource{
							Ref: fakeResolvedRef,
						},
					},
				},
			},
			catalogCache: mockCatalogCache{
				getFunc: func(catalogName, resolvedRef string) (fs.FS, error) {
					assert.Equal(t, catalogKey.Name, catalogName)
					assert.Equal(t, fakeResolvedRef, resolvedRef)
					return nil, errors.New("fake error from cache get function")
				},
			},
			wantGetCacheCalled:      true,
			wantPopulateCacheCalled: true,
		},
		{
			name: "catalog does not exist",
			catalogCache: mockCatalogCache{
				removeFunc: func(catalogName string) error {
					assert.Equal(t, catalogKey.Name, catalogName)
					return nil
				},
			},
			wantRemoveCacheCalled: true,
		},
		{
			name: "catalog does not exist - error on removal",
			catalogCache: mockCatalogCache{
				removeFunc: func(catalogName string) error {
					assert.Equal(t, catalogKey.Name, catalogName)
					return errors.New("fake error from remove")
				},
			},
			wantRemoveCacheCalled: true,
			wantErr:               "error removing cache for catalog",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			clientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tt.catalog != nil {
				clientBuilder = clientBuilder.WithObjects(tt.catalog)
			}
			cl := clientBuilder.Build()

			reconciler := &controllers.ClusterCatalogReconciler{
				Client:                cl,
				CatalogCache:          controllers.CatalogCache(&tt.catalogCache),
				CatalogCachePopulator: controllers.CatalogCachePopulator(&tt.catalogCachePopulator),
			}

			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: catalogKey})
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
			require.Equal(t, ctrl.Result{}, result)

			assert.Equal(t, tt.wantRemoveCacheCalled, tt.catalogCache.removeFuncCalled)
			assert.Equal(t, tt.wantGetCacheCalled, tt.catalogCache.getFuncCalled)
			assert.Equal(t, tt.wantPopulateCacheCalled, tt.catalogCachePopulator.populateCacheCalled)
		})
	}
}

type mockCatalogCache struct {
	removeFuncCalled bool
	removeFunc       func(catalogName string) error
	getFuncCalled    bool
	getFunc          func(catalogName, resolvedRef string) (fs.FS, error)
}

func (m *mockCatalogCache) Remove(catalogName string) error {
	m.removeFuncCalled = true
	if m.removeFunc != nil {
		return m.removeFunc(catalogName)
	}

	return nil
}

func (m *mockCatalogCache) Get(catalogName, resolvedRef string) (fs.FS, error) {
	m.getFuncCalled = true
	if m.getFunc != nil {
		return m.getFunc(catalogName, resolvedRef)
	}

	return nil, nil
}

type mockCatalogCachePopulator struct {
	populateCacheCalled bool
	populateCacheFunc   func(ctx context.Context, catalog *catalogd.ClusterCatalog) (fs.FS, error)
}

func (m *mockCatalogCachePopulator) PopulateCache(ctx context.Context, catalog *catalogd.ClusterCatalog) (fs.FS, error) {
	m.populateCacheCalled = true
	if m.populateCacheFunc != nil {
		return m.populateCacheFunc(ctx, catalog)
	}

	return nil, nil
}
