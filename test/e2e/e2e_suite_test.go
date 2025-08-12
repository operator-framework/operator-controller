package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/scheme"
	utils "github.com/operator-framework/operator-controller/test/utils"
)

var (
	cfg *rest.Config
	c   client.Client
)

const (
	testSummaryOutputEnvVar = "GITHUB_STEP_SUMMARY"
	testCatalogRefEnvVar    = "CATALOG_IMG"
	testCatalogName         = "test-catalog"
	latestImageTag          = "latest"
)

func TestMain(m *testing.M) {
	cfg = ctrl.GetConfigOrDie()

	var err error
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme.Scheme))
	c, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	utilruntime.Must(err)
	ctrl.SetLogger(klog.NewKlogr())

	res := m.Run()
	err = utils.PrintSummary(testSummaryOutputEnvVar)
	if err != nil {
		fmt.Println("PrintSummary error", err)
	}
	os.Exit(res)
}

// createTestCatalog will create a new catalog on the test cluster, provided
// the context, catalog name, and the image reference. It returns the created catalog
// or an error if any errors occurred while creating the catalog.
// Note that catalogd will automatically create the label:
//
//	"olm.operatorframework.io/metadata.name": name
func createTestCatalog(ctx context.Context, name string, imageRef string) (*ocv1.ClusterCatalog, error) {
	catalog := &ocv1.ClusterCatalog{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: ocv1.ClusterCatalogSpec{
			Source: ocv1.CatalogSource{
				Type: ocv1.SourceTypeImage,
				Image: &ocv1.ImageSource{
					Ref:                 imageRef,
					PollIntervalMinutes: ptr.To(1),
				},
			},
		},
	}

	err := c.Create(ctx, catalog)
	return catalog, err
}

// patchTestCatalog will patch the existing clusterCatalog on the test cluster, provided
// the context, catalog name, and the image reference. It returns an error
// if any errors occurred while updating the catalog.
func patchTestCatalog(ctx context.Context, name string, newImageRef string) error {
	// Fetch the existing ClusterCatalog
	catalog := &ocv1.ClusterCatalog{}
	err := c.Get(ctx, client.ObjectKey{Name: name}, catalog)
	if err != nil {
		return err
	}

	// Update the ImageRef
	catalog.Spec.Source.Image.Ref = newImageRef

	// Patch the ClusterCatalog
	err = c.Update(ctx, catalog)
	if err != nil {
		return err
	}

	return err
}
