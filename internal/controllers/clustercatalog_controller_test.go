package controllers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/controllers"
	"github.com/operator-framework/operator-controller/internal/scheme"
)

func TestClusterCatalogReconcilerFinalizers(t *testing.T) {
	catalogKey := types.NamespacedName{Name: "test-catalog"}

	for _, tt := range []struct {
		name                  string
		catalog               *catalogd.ClusterCatalog
		cacheRemoveFunc       func(catalogName string) error
		wantCacheRemoveCalled bool
		wantErr               string
	}{
		{
			name: "catalog exists",
			catalog: &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: catalogKey.Name,
				},
			},
		},
		{
			name: "catalog does not exist",
			cacheRemoveFunc: func(catalogName string) error {
				assert.Equal(t, catalogKey.Name, catalogName)
				return nil
			},
			wantCacheRemoveCalled: true,
		},
		{
			name: "catalog does not exist - error on removal",
			cacheRemoveFunc: func(catalogName string) error {
				return errors.New("fake error from remove")
			},
			wantCacheRemoveCalled: true,
			wantErr:               "fake error from remove",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			clientBuilder := fake.NewClientBuilder().WithScheme(scheme.Scheme)
			if tt.catalog != nil {
				clientBuilder = clientBuilder.WithObjects(tt.catalog)
			}
			cl := clientBuilder.Build()

			cacheRemover := &mockCatalogCacheRemover{
				removeFunc: tt.cacheRemoveFunc,
			}

			reconciler := &controllers.ClusterCatalogReconciler{
				Client: cl,
				Cache:  cacheRemover,
			}

			result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: catalogKey})
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
			require.Equal(t, ctrl.Result{}, result)

			assert.Equal(t, tt.wantCacheRemoveCalled, cacheRemover.called)
		})
	}
}

type mockCatalogCacheRemover struct {
	called     bool
	removeFunc func(catalogName string) error
}

func (m *mockCatalogCacheRemover) Remove(catalogName string) error {
	m.called = true
	return m.removeFunc(catalogName)
}
