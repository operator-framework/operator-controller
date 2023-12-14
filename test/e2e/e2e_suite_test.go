package e2e

import (
	"context"
	"os"
	"testing"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

var (
	cfg *rest.Config
	c   client.Client
)

const (
	testCatalogRefEnvVar = "CATALOG_IMG"
	testCatalogName      = "test-catalog"
)

func TestMain(m *testing.M) {
	cfg = ctrl.GetConfigOrDie()

	scheme := runtime.NewScheme()

	utilruntime.Must(ocv1alpha1.AddToScheme(scheme))
	utilruntime.Must(rukpakv1alpha1.AddToScheme(scheme))
	utilruntime.Must(catalogd.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))

	var err error
	c, err = client.New(cfg, client.Options{Scheme: scheme})
	utilruntime.Must(err)

	os.Exit(m.Run())
}

// createTestCatalog will create a new catalog on the test cluster, provided
// the context, catalog name, and the image reference. It returns the created catalog
// or an error if any errors occurred while creating the catalog.
func createTestCatalog(ctx context.Context, name string, imageRef string) (*catalogd.Catalog, error) {
	catalog := &catalogd.Catalog{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: catalogd.CatalogSpec{
			Source: catalogd.CatalogSource{
				Type: catalogd.SourceTypeImage,
				Image: &catalogd.ImageSource{
					Ref:                   imageRef,
					InsecureSkipTLSVerify: true,
				},
			},
		},
	}

	err := c.Create(ctx, catalog)
	return catalog, err
}
