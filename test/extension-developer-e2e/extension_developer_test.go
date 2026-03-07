package extensione2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestExtensionDeveloper(t *testing.T) {
	t.Parallel()
	cfg := ctrl.GetConfigOrDie()

	scheme := runtime.NewScheme()

	require.NoError(t, ocv1.AddToScheme(scheme))

	require.NotEmpty(t, os.Getenv("CATALOG_IMG"), "environment variable CATALOG_IMG must be set")
	require.NotEmpty(t, os.Getenv("REG_PKG_NAME"), "environment variable REG_PKG_NAME must be set")

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	ctx := context.Background()

	catalog := &ocv1.ClusterCatalog{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "catalog",
		},
		Spec: ocv1.ClusterCatalogSpec{
			Source: ocv1.CatalogSource{
				Type: ocv1.SourceTypeImage,
				Image: &ocv1.ImageSource{
					Ref: os.Getenv("CATALOG_IMG"),
				},
			},
		},
	}
	require.NoError(t, c.Create(ctx, catalog))

	installNamespace := "default"

	clusterExtension := &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "registryv1",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Source: ocv1.SourceConfig{
				SourceType: "Catalog",
				Catalog: &ocv1.CatalogFilter{
					PackageName: os.Getenv("REG_PKG_NAME"),
				},
			},
			Namespace: installNamespace,
		},
	}

	t.Logf("When creating an ClusterExtension that references a package with a %q bundle type", clusterExtension.Name)
	require.NoError(t, c.Create(ctx, clusterExtension))
	t.Log("It should have a status condition type of Installed with a status of True and a reason of Success")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		ext := &ocv1.ClusterExtension{}
		require.NoError(ct, c.Get(ctx, client.ObjectKeyFromObject(clusterExtension), ext))
		cond := meta.FindStatusCondition(ext.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, 2*time.Minute, time.Second)
	require.NoError(t, c.Delete(ctx, catalog))
	require.NoError(t, c.Delete(ctx, clusterExtension))
}
