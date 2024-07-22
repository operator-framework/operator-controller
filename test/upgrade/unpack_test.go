package upgradee2e

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/catalogd/test/e2e"
)

var _ = Describe("ClusterCatalog Unpacking", func() {
	When("A ClusterCatalog is created", func() {
		It("Successfully unpacks catalog contents", func() {
			ctx := context.Background()
			catalog := &catalogd.ClusterCatalog{}
			By("Ensuring ClusterCatalog has Status.Condition of Unpacked with a status == True")
			Eventually(func(g Gomega) {
				err := c.Get(ctx, types.NamespacedName{Name: testClusterCatalogName}, catalog)
				g.Expect(err).ToNot(HaveOccurred())
				cond := meta.FindStatusCondition(catalog.Status.Conditions, catalogd.TypeUnpacked)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(catalogd.ReasonUnpackSuccessful))
			}).Should(Succeed())

			expectedFBC, err := os.ReadFile("../../testdata/catalogs/test-catalog/expected_all.json")
			Expect(err).To(Not(HaveOccurred()))

			By("Making sure the catalog content is available via the http server")
			Eventually(func(g Gomega) {
				actualFBC, err := e2e.ReadTestCatalogServerContents(ctx, catalog, c, kubeClient)
				g.Expect(err).To(Not(HaveOccurred()))
				g.Expect(cmp.Diff(expectedFBC, actualFBC)).To(BeEmpty())
			}).Should(Succeed())

		})
	})
})
