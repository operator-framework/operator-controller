package e2e

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
)

var (
	cfg             *rest.Config
	c               client.Client
	operatorCatalog *catalogd.Catalog
)

const (
	testCatalogRef  = "localhost/testdata/catalogs/test-catalog:e2e"
	testCatalogName = "test-catalog"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	SetDefaultEventuallyTimeout(1 * time.Minute)
	SetDefaultEventuallyPollingInterval(1 * time.Second)
	RunSpecs(t, "E2E Suite")
}

var _ = BeforeSuite(func() {
	cfg = ctrl.GetConfigOrDie()

	scheme := runtime.NewScheme()
	err := operatorv1alpha1.AddToScheme(scheme)
	Expect(err).To(Not(HaveOccurred()))

	err = rukpakv1alpha1.AddToScheme(scheme)
	Expect(err).To(Not(HaveOccurred()))

	err = catalogd.AddToScheme(scheme)
	Expect(err).ToNot(HaveOccurred())
	c, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).To(Not(HaveOccurred()))

	ctx := context.Background()
	operatorCatalog, err = createTestCatalog(ctx, testCatalogName, testCatalogRef)
	Expect(err).ToNot(HaveOccurred())

	Eventually(func(g Gomega) {
		err := c.Get(ctx, types.NamespacedName{Name: operatorCatalog.Name}, operatorCatalog)
		g.Expect(err).ToNot(HaveOccurred())
		cond := apimeta.FindStatusCondition(operatorCatalog.Status.Conditions, catalogd.TypeUnpacked)
		g.Expect(cond).ToNot(BeNil())
		g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(cond.Reason).To(Equal(catalogd.ReasonUnpackSuccessful))

		// Ensure some packages exist before continuing so the
		// operators don't get stuck in a bad state
		pList := &catalogd.PackageList{}
		err = c.List(ctx, pList)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(pList.Items).To(HaveLen(2))
	}).Should(Succeed())
})

var _ = AfterSuite(func() {
	ctx := context.Background()

	Expect(c.Delete(ctx, operatorCatalog)).To(Succeed())
	Eventually(func(g Gomega) {
		err := c.Get(ctx, types.NamespacedName{Name: operatorCatalog.Name}, &catalogd.Catalog{})
		Expect(errors.IsNotFound(err)).To(BeTrue())
	}).Should(Succeed())

	// speed up delete without waiting for gc
	Expect(c.DeleteAllOf(ctx, &catalogd.BundleMetadata{})).To(Succeed())
	Expect(c.DeleteAllOf(ctx, &catalogd.Package{})).To(Succeed())

	Eventually(func(g Gomega) {
		// ensure resource cleanup
		packages := &catalogd.PackageList{}
		g.Expect(c.List(ctx, packages)).To(Succeed())
		g.Expect(packages.Items).To(BeEmpty())

		bmd := &catalogd.BundleMetadataList{}
		g.Expect(c.List(ctx, bmd)).To(Succeed())
		g.Expect(bmd.Items).To(BeEmpty())
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
