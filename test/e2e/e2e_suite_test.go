package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/scheme"
	utils "github.com/operator-framework/operator-controller/internal/shared/util/testutils"
)

var (
	cfg *rest.Config
	c   client.Client
)

const (
	testSummaryOutputEnvVar = "E2E_SUMMARY_OUTPUT"
	latestImageTag          = "latest"
)

func TestMain(m *testing.M) {
	cfg = ctrl.GetConfigOrDie()

	var err error
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme.Scheme))
	c, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	utilruntime.Must(err)

	res := m.Run()
	path := os.Getenv(testSummaryOutputEnvVar)
	if path == "" {
		fmt.Printf("Note: E2E_SUMMARY_OUTPUT is unset; skipping summary generation")
	} else {
		err = utils.PrintSummary(path)
		if err != nil {
			// Fail the run if alerts are found
			fmt.Printf("%v", err)
			os.Exit(1)
		}
	}
	os.Exit(res)
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
