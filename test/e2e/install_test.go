package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	catalogd "github.com/operator-framework/catalogd/pkg/apis/core/v1beta1"
	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
)

const (
	defaultTimeout = 30 * time.Second
	defaultPoll    = 1 * time.Second
)

var _ = Describe("Operator Install", func() {
	var (
		ctx             context.Context
		pkgName         string
		operatorName    string
		operator        *operatorv1alpha1.Operator
		operatorCatalog *catalogd.CatalogSource
	)
	When("An operator is installed from an operator catalog", func() {
		BeforeEach(func() {
			ctx = context.Background()
			pkgName = "argocd-operator"
			operatorName = fmt.Sprintf("operator-%s", rand.String(8))
			operator = &operatorv1alpha1.Operator{
				ObjectMeta: metav1.ObjectMeta{
					Name: operatorName,
				},
				Spec: operatorv1alpha1.OperatorSpec{
					PackageName: pkgName,
				},
			}
			operatorCatalog = &catalogd.CatalogSource{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-catalog",
				},
				Spec: catalogd.CatalogSourceSpec{
					// (TODO): Set up a local image registry, and build and store a test catalog in it
					// to use in the test suite
					Image: "quay.io/operatorhubio/catalog:latest",
				},
			}
			err := c.Create(ctx, operatorCatalog)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: "test-catalog"}, operatorCatalog)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(operatorCatalog.Status.Conditions)).To(Equal(1))
				g.Expect(operatorCatalog.Status.Conditions[0].Message).To(Equal("catalog contents have been unpacked and are available on cluster"))
			}).WithTimeout(5 * time.Minute).WithPolling(defaultPoll).Should(Succeed())
		})
		It("resolves the specified package with correct bundle path", func() {
			By("creating the Operator resource")
			err := c.Create(ctx, operator)
			Expect(err).ToNot(HaveOccurred())

			By("eventually reporting a successful resolution and bundle path")
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(operator.Status.Conditions)).To(Equal(1))
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
				g.Expect(cond.Message).To(ContainSubstring("resolved to"))
				g.Expect(operator.Status.ResolvedBundleResource).ToNot(BeEmpty())
			}).WithTimeout(defaultTimeout).WithPolling(defaultPoll).Should(Succeed())

			By("eventually installing the package successfully")
			Eventually(func(g Gomega) {
				bd := rukpakv1alpha1.BundleDeployment{}
				err = c.Get(ctx, types.NamespacedName{Name: operatorName}, &bd)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(bd.Status.Conditions)).To(Equal(2))
				g.Expect(bd.Status.Conditions[0].Reason).To(Equal("UnpackSuccessful"))
				g.Expect(bd.Status.Conditions[1].Reason).To(Equal("InstallationSucceeded"))
			}).WithTimeout(defaultTimeout).WithPolling(defaultPoll).Should(Succeed())

		})
		AfterEach(func() {
			err := c.Delete(ctx, operatorCatalog)
			Expect(err).ToNot(HaveOccurred())
			err = c.Delete(ctx, operator)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
