package e2e

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
)

const (
	catalogRefEnvVar = "TEST_CATALOG_IMAGE"
	catalogName      = "test-catalog"
	pkg              = "prometheus"
	version          = "0.47.0"
	channel          = "beta"
	bundle           = "prometheus-operator.0.47.0"
	bundleImage      = "localhost/testdata/bundles/registry-v1/prometheus-operator:v0.47.0"
)

// catalogImageRef returns the image reference for the test catalog image, defaulting to the value of the environment
// variable TEST_CATALOG_IMAGE if set, falling back to docker-registry.catalogd-e2e.svc:5000/test-catalog:e2e otherwise.
func catalogImageRef() string {
	if s := os.Getenv(catalogRefEnvVar); s != "" {
		return s
	}

	return "docker-registry.catalogd-e2e.svc:5000/test-catalog:e2e"
}

var _ = Describe("ClusterCatalog Unpacking", func() {
	var (
		ctx     context.Context
		catalog *catalogd.ClusterCatalog
	)
	When("A ClusterCatalog is created", func() {
		BeforeEach(func() {
			ctx = context.Background()
			var err error

			catalog = &catalogd.ClusterCatalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: catalogName,
				},
				Spec: catalogd.ClusterCatalogSpec{
					Source: catalogd.CatalogSource{
						Type: catalogd.SourceTypeImage,
						Image: &catalogd.ImageSource{
							Ref: catalogImageRef(),
						},
					},
				},
			}

			err = c.Create(ctx, catalog)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Successfully unpacks catalog contents", func() {
			By("Ensuring ClusterCatalog has Status.Condition of Progressing with a status == False and reason == Succeeded")
			Eventually(func(g Gomega) {
				err := c.Get(ctx, types.NamespacedName{Name: catalog.Name}, catalog)
				g.Expect(err).ToNot(HaveOccurred())
				cond := meta.FindStatusCondition(catalog.Status.Conditions, catalogd.TypeProgressing)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(catalogd.ReasonSucceeded))
			}).Should(Succeed())

			By("Checking that it has an appropriate name label")
			Expect(catalog.ObjectMeta.Labels).To(Not(BeNil()))
			Expect(catalog.ObjectMeta.Labels).To(Not(BeEmpty()))
			Expect(catalog.ObjectMeta.Labels).To(HaveKeyWithValue("olm.operatorframework.io/metadata.name", catalogName))

			By("Making sure the catalog content is available via the http server")
			actualFBC, err := ReadTestCatalogServerContents(ctx, catalog, c, kubeClient)
			Expect(err).To(Not(HaveOccurred()))

			expectedFBC, err := os.ReadFile("../../testdata/catalogs/test-catalog/expected_all.json")
			Expect(err).To(Not(HaveOccurred()))
			Expect(cmp.Diff(expectedFBC, actualFBC)).To(BeEmpty())

			By("Ensuring ClusterCatalog has Status.Condition of Type = Serving with a status == True")
			Eventually(func(g Gomega) {
				err := c.Get(ctx, types.NamespacedName{Name: catalog.Name}, catalog)
				g.Expect(err).ToNot(HaveOccurred())
				cond := meta.FindStatusCondition(catalog.Status.Conditions, catalogd.TypeServing)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(catalogd.ReasonAvailable))
			}).Should(Succeed())
		})
		AfterEach(func() {
			Expect(c.Delete(ctx, catalog)).To(Succeed())
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: catalog.Name}, &catalogd.ClusterCatalog{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		})
	})
})
