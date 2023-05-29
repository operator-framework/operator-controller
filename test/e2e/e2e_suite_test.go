package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/env"

	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

var (
	cfg             *rest.Config
	c               client.Client
	operatorCatalog *catalogd.Catalog
)

const (
	testCatalogRefEnvVar  = "TEST_CATALOG_IMAGE"
	testCatalogRefDefault = "localhost/testdata/catalogs/test-catalog:e2e"
	testCatalogName       = "test-catalog"
)

// returns the image reference for the test, checking for environment variable substitution, with a default of localhost/testdata/catalogs/test-catalog:e2e
func getCatalogImageRef() string {
	if s := os.Getenv(testCatalogRefEnvVar); s != "" {
		return s
	}
	return testCatalogRefDefault
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	SetDefaultEventuallyTimeout(1 * time.Minute)
	SetDefaultEventuallyPollingInterval(1 * time.Second)
	RunSpecs(t, "E2E Suite")
}

var _ = BeforeSuite(func() {
	cfg = ctrl.GetConfigOrDie()

	scheme := runtime.NewScheme()

	Expect(operatorv1alpha1.AddToScheme(scheme)).To(Succeed())
	Expect(rukpakv1alpha1.AddToScheme(scheme)).To(Succeed())
	Expect(catalogd.AddToScheme(scheme)).To(Succeed())

	var err error

	err = appsv1.AddToScheme(scheme)
	Expect(err).ToNot(HaveOccurred())

	err = corev1.AddToScheme(scheme)
	Expect(err).ToNot(HaveOccurred())

	c, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).To(Not(HaveOccurred()))

	ctx := context.Background()
	operatorCatalog, err = createTestCatalog(ctx, testCatalogName, getCatalogImageRef())
	Expect(err).ToNot(HaveOccurred())

	// TODO: REMOVE THIS. This should not be necessary if we have idiomatic APIs. Kubernetes is supposed
	//   to be eventually consistent. Waits for preconditions like this hide bugs.
	Eventually(func(g Gomega) {
		err := c.Get(ctx, types.NamespacedName{Name: operatorCatalog.Name}, operatorCatalog)
		g.Expect(err).ToNot(HaveOccurred())
		cond := apimeta.FindStatusCondition(operatorCatalog.Status.Conditions, catalogd.TypeUnpacked)
		g.Expect(cond).ToNot(BeNil())
		g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(cond.Reason).To(Equal(catalogd.ReasonUnpackSuccessful))
	}).Should(Succeed())
})

var _ = AfterSuite(func() {
	ctx := context.Background()
	if basePath := env.GetString("ARTIFACT_PATH", ""); basePath != "" {
		// get all the artifacts from the test run and save them to the artifact path
		getArtifactsOutput(ctx, basePath)
	}
	Expect(c.Delete(ctx, operatorCatalog)).To(Succeed())
	Eventually(func(g Gomega) {
		err := c.Get(ctx, types.NamespacedName{Name: operatorCatalog.Name}, &catalogd.Catalog{})
		Expect(errors.IsNotFound(err)).To(BeTrue())
	}).Should(Succeed())

	// speed up delete without waiting for gc
	Expect(c.DeleteAllOf(ctx, &catalogd.CatalogMetadata{})).To(Succeed())

	Eventually(func(g Gomega) {
		// ensure resource cleanup
		cmd := &catalogd.CatalogMetadataList{}
		g.Expect(c.List(ctx, cmd)).To(Succeed())
		g.Expect(cmd.Items).To(BeEmpty())
	}).Should(Succeed())
})

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
					Ref: imageRef,
				},
			},
		},
	}

	err := c.Create(ctx, catalog)
	return catalog, err
}
