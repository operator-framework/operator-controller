package controllers_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/scheme"
	mockcontrollers "github.com/operator-framework/operator-controller/internal/testutil/mock/controllers"
)

func TestClusterCatalogReconcilerFinalizers(t *testing.T) {
	const fakeResolvedRef = "fake/catalog@sha256:fakesha1"
	catalogKey := types.NamespacedName{Name: "test-catalog"}

	for _, tt := range []struct {
		name       string
		catalog    *ocv1.ClusterCatalog
		setupMocks func(*gomock.Controller) (controllers.CatalogCache, controllers.CatalogCachePopulator)
		wantErr    string
	}{
		{
			name: "catalog exists - cache unpopulated",
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: catalogKey.Name,
				},
				Status: ocv1.ClusterCatalogStatus{
					ResolvedSource: &ocv1.ResolvedCatalogSource{
						Image: &ocv1.ResolvedImageSource{
							Ref: fakeResolvedRef,
						},
					},
				},
			},
			setupMocks: func(ctrl *gomock.Controller) (controllers.CatalogCache, controllers.CatalogCachePopulator) {
				cache := mockcontrollers.NewMockCatalogCache(ctrl)
				cache.EXPECT().Get(catalogKey.Name, fakeResolvedRef).Return(nil, nil)
				populator := mockcontrollers.NewMockCatalogCachePopulator(ctrl)
				populator.EXPECT().PopulateCache(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, catalog *ocv1.ClusterCatalog) (fs.FS, error) {
						assert.Equal(t, catalogKey.Name, catalog.Name)
						return nil, nil
					},
				)
				return cache, populator
			},
		},
		{
			name: "catalog exists - cache already populated",
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: catalogKey.Name,
				},
				Status: ocv1.ClusterCatalogStatus{
					ResolvedSource: &ocv1.ResolvedCatalogSource{
						Image: &ocv1.ResolvedImageSource{
							Ref: fakeResolvedRef,
						},
					},
				},
			},
			setupMocks: func(ctrl *gomock.Controller) (controllers.CatalogCache, controllers.CatalogCachePopulator) {
				cache := mockcontrollers.NewMockCatalogCache(ctrl)
				cache.EXPECT().Get(catalogKey.Name, fakeResolvedRef).DoAndReturn(
					func(catalogName, resolvedRef string) (fs.FS, error) {
						assert.Equal(t, catalogKey.Name, catalogName)
						assert.Equal(t, fakeResolvedRef, resolvedRef)
						// Just any non-nil fs.FS to simulate existence of cache
						return fstest.MapFS{}, nil
					},
				)
				populator := mockcontrollers.NewMockCatalogCachePopulator(ctrl)
				return cache, populator
			},
		},
		{
			name: "catalog exists - catalog not yet resolved",
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: catalogKey.Name,
				},
			},
			setupMocks: func(ctrl *gomock.Controller) (controllers.CatalogCache, controllers.CatalogCachePopulator) {
				cache := mockcontrollers.NewMockCatalogCache(ctrl)
				populator := mockcontrollers.NewMockCatalogCachePopulator(ctrl)
				return cache, populator
			},
		},
		{
			name: "catalog exists - error on cache population",
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: catalogKey.Name,
				},
				Status: ocv1.ClusterCatalogStatus{
					ResolvedSource: &ocv1.ResolvedCatalogSource{
						Image: &ocv1.ResolvedImageSource{
							Ref: fakeResolvedRef,
						},
					},
				},
			},
			setupMocks: func(ctrl *gomock.Controller) (controllers.CatalogCache, controllers.CatalogCachePopulator) {
				cache := mockcontrollers.NewMockCatalogCache(ctrl)
				cache.EXPECT().Get(catalogKey.Name, fakeResolvedRef).Return(nil, nil)
				populator := mockcontrollers.NewMockCatalogCachePopulator(ctrl)
				populator.EXPECT().PopulateCache(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, catalog *ocv1.ClusterCatalog) (fs.FS, error) {
						assert.Equal(t, catalogKey.Name, catalog.Name)
						return nil, errors.New("fake error from populate cache function")
					},
				)
				return cache, populator
			},
			wantErr: "error populating cache for catalog",
		},
		{
			name: "catalog exists - error on cache get",
			catalog: &ocv1.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: catalogKey.Name,
				},
				Status: ocv1.ClusterCatalogStatus{
					ResolvedSource: &ocv1.ResolvedCatalogSource{
						Image: &ocv1.ResolvedImageSource{
							Ref: fakeResolvedRef,
						},
					},
				},
			},
			setupMocks: func(ctrl *gomock.Controller) (controllers.CatalogCache, controllers.CatalogCachePopulator) {
				cache := mockcontrollers.NewMockCatalogCache(ctrl)
				cache.EXPECT().Get(catalogKey.Name, fakeResolvedRef).DoAndReturn(
					func(catalogName, resolvedRef string) (fs.FS, error) {
						assert.Equal(t, catalogKey.Name, catalogName)
						assert.Equal(t, fakeResolvedRef, resolvedRef)
						return nil, errors.New("fake error from cache get function")
					},
				)
				populator := mockcontrollers.NewMockCatalogCachePopulator(ctrl)
				populator.EXPECT().PopulateCache(gomock.Any(), gomock.Any()).Return(nil, nil)
				return cache, populator
			},
		},
		{
			name: "catalog does not exist",
			setupMocks: func(ctrl *gomock.Controller) (controllers.CatalogCache, controllers.CatalogCachePopulator) {
				cache := mockcontrollers.NewMockCatalogCache(ctrl)
				cache.EXPECT().Remove(catalogKey.Name).DoAndReturn(
					func(catalogName string) error {
						assert.Equal(t, catalogKey.Name, catalogName)
						return nil
					},
				)
				populator := mockcontrollers.NewMockCatalogCachePopulator(ctrl)
				return cache, populator
			},
		},
		{
			name: "catalog does not exist - error on removal",
			setupMocks: func(ctrl *gomock.Controller) (controllers.CatalogCache, controllers.CatalogCachePopulator) {
				cache := mockcontrollers.NewMockCatalogCache(ctrl)
				cache.EXPECT().Remove(catalogKey.Name).DoAndReturn(
					func(catalogName string) error {
						assert.Equal(t, catalogKey.Name, catalogName)
						return errors.New("fake error from remove")
					},
				)
				populator := mockcontrollers.NewMockCatalogCachePopulator(ctrl)
				return cache, populator
			},
			wantErr: "error removing cache for catalog",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			mockCtrl := gomock.NewController(t)
			cache, populator := tt.setupMocks(mockCtrl)

			clientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tt.catalog != nil {
				clientBuilder = clientBuilder.WithObjects(tt.catalog)
			}
			cl := clientBuilder.Build()

			reconciler := &controllers.ClusterCatalogReconciler{
				Client:                cl,
				CatalogCache:          cache,
				CatalogCachePopulator: populator,
			}

			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: catalogKey})
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
			require.Equal(t, ctrl.Result{}, result)
		})
	}
}
