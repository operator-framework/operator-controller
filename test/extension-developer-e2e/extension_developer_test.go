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

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

func TestExtensionDeveloper(t *testing.T) {
	t.Parallel()
	cfg := ctrl.GetConfigOrDie()

	scheme := runtime.NewScheme()

	require.NoError(t, catalogd.AddToScheme(scheme))
	require.NoError(t, ocv1alpha1.AddToScheme(scheme))

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	var clusterExtensions = []*ocv1alpha1.ClusterExtension{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "plainv0",
			},
			Spec: ocv1alpha1.ClusterExtensionSpec{
				PackageName:      os.Getenv("PLAIN_PKG_NAME"),
				InstallNamespace: "default",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "registryv1",
			},
			Spec: ocv1alpha1.ClusterExtensionSpec{
				PackageName:      os.Getenv("REG_PKG_NAME"),
				InstallNamespace: "default",
			},
		},
	}

	for _, ce := range clusterExtensions {
		clusterExtension := ce
		t.Run(clusterExtension.ObjectMeta.Name, func(t *testing.T) {
			t.Parallel()
			catalog := &catalogd.Catalog{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "catalog",
				},
				Spec: catalogd.CatalogSpec{
					Source: catalogd.CatalogSource{
						Type: catalogd.SourceTypeImage,
						Image: &catalogd.ImageSource{
							Ref:                   os.Getenv("CATALOG_IMG"),
							InsecureSkipTLSVerify: true,
						},
					},
				},
			}
			t.Logf("When creating an ClusterExtension that references a package with a %q bundle type", clusterExtension.ObjectMeta.Name)
			require.NoError(t, c.Create(context.Background(), catalog))
			require.NoError(t, c.Create(context.Background(), clusterExtension))
			t.Log("It should have a status condition type of Installed with a status of True and a reason of Success")
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				ext := &ocv1alpha1.ClusterExtension{}
				assert.NoError(ct, c.Get(context.Background(), client.ObjectKeyFromObject(clusterExtension), ext))
				cond := meta.FindStatusCondition(ext.Status.Conditions, ocv1alpha1.TypeInstalled)
				if !assert.NotNil(ct, cond) {
					return
				}
				assert.Equal(ct, metav1.ConditionTrue, cond.Status)
				assert.Equal(ct, ocv1alpha1.ReasonSuccess, cond.Reason)
			}, 2*time.Minute, time.Second)
			require.NoError(t, c.Delete(context.Background(), catalog))
			require.NoError(t, c.Delete(context.Background(), clusterExtension))
		})
	}
}
