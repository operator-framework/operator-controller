package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

var _ = Describe("Operator Install", func() {
	var (
		ctx          context.Context
		operatorName string
		operator     *operatorv1alpha1.Operator
	)
	When("An operator is installed from an operator catalog", func() {
		BeforeEach(func() {
			ctx = context.Background()
			operatorName = fmt.Sprintf("operator-%s", rand.String(8))
			operator = &operatorv1alpha1.Operator{
				ObjectMeta: metav1.ObjectMeta{
					Name: operatorName,
				},
			}
		})
		When("the operator bundle format is registry+v1", func() {
			BeforeEach(func() {
				operator.Spec = operatorv1alpha1.OperatorSpec{
					PackageName: "prometheus",
					Version:     "0.47.0",
				}
			})
			It("resolves the specified package with correct bundle path", func() {
				By("creating the Operator resource")
				Expect(c.Create(ctx, operator)).To(Succeed())

				By("eventually reporting a successful resolution and bundle path")
				Eventually(func(g Gomega) {
					g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
					g.Expect(operator.Status.Conditions).To(HaveLen(2))
					cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
					g.Expect(cond).ToNot(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
					g.Expect(cond.Message).To(ContainSubstring("resolved to"))
					g.Expect(operator.Status.ResolvedBundleResource).ToNot(BeEmpty())
				}).Should(Succeed())

				By("eventually installing the package successfully")
				Eventually(func(g Gomega) {
					g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
					cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeInstalled)
					g.Expect(cond).ToNot(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
					g.Expect(cond.Message).To(ContainSubstring("installed from"))
					g.Expect(operator.Status.InstalledBundleResource).ToNot(BeEmpty())

					bd := rukpakv1alpha1.BundleDeployment{}
					g.Expect(c.Get(ctx, types.NamespacedName{Name: operatorName}, &bd)).To(Succeed())
					g.Expect(bd.Status.Conditions).To(HaveLen(2))
					g.Expect(bd.Status.Conditions[0].Reason).To(Equal("UnpackSuccessful"))
					g.Expect(bd.Status.Conditions[1].Reason).To(Equal("InstallationSucceeded"))
				}).Should(Succeed())
			})
		})

		When("the operator bundle format is plain+v0", func() {
			BeforeEach(func() {
				operator.Spec = operatorv1alpha1.OperatorSpec{
					PackageName: "plain",
				}
			})
			It("resolves the specified package with correct bundle path", func() {
				By("creating the Operator resource")
				Expect(c.Create(ctx, operator)).To(Succeed())

				By("eventually reporting a successful resolution and bundle path")
				Eventually(func(g Gomega) {
					g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
					g.Expect(operator.Status.Conditions).To(HaveLen(2))
					cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
					g.Expect(cond).ToNot(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
					g.Expect(cond.Message).To(ContainSubstring("resolved to"))
					g.Expect(operator.Status.ResolvedBundleResource).ToNot(BeEmpty())
				}).Should(Succeed())

				By("eventually installing the package successfully")
				Eventually(func(g Gomega) {
					g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
					cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeInstalled)
					g.Expect(cond).ToNot(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
					g.Expect(cond.Message).To(ContainSubstring("installed from"))
					g.Expect(operator.Status.InstalledBundleResource).ToNot(BeEmpty())

					bd := rukpakv1alpha1.BundleDeployment{}
					g.Expect(c.Get(ctx, types.NamespacedName{Name: operatorName}, &bd)).To(Succeed())
					g.Expect(bd.Status.Conditions).To(HaveLen(2))
					g.Expect(bd.Status.Conditions[0].Reason).To(Equal("UnpackSuccessful"))
					g.Expect(bd.Status.Conditions[1].Reason).To(Equal("InstallationSucceeded"))
				}).Should(Succeed())
			})
		})

		It("resolves again when a new catalog is available", func() {
			pkgName := "prometheus"
			operator.Spec = operatorv1alpha1.OperatorSpec{
				PackageName: pkgName,
			}

			// Delete the catalog first
			Expect(c.Delete(ctx, operatorCatalog)).To(Succeed())

			Eventually(func(g Gomega) {
				// target package should not be present on cluster
				err := c.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s-%s", operatorCatalog.Name, declcfg.SchemaPackage, pkgName)}, &catalogd.CatalogMetadata{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())

			By("creating the Operator resource")
			Expect(c.Create(ctx, operator)).To(Succeed())

			By("failing to find Operator during resolution")
			Eventually(func(g Gomega) {
				g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonResolutionFailed))
				g.Expect(cond.Message).To(Equal(fmt.Sprintf("package '%s' not found", pkgName)))
			}).Should(Succeed())

			By("creating an Operator catalog with the desired package")
			var err error
			operatorCatalog, err = createTestCatalog(ctx, testCatalogName, getCatalogImageRef())
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				g.Expect(c.Get(ctx, types.NamespacedName{Name: operatorCatalog.Name}, operatorCatalog)).To(Succeed())
				cond := apimeta.FindStatusCondition(operatorCatalog.Status.Conditions, catalogd.TypeUnpacked)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(catalogd.ReasonUnpackSuccessful))
			}).Should(Succeed())

			By("eventually resolving the package successfully")
			Eventually(func(g Gomega) {
				g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
			}).Should(Succeed())
		})

		It("handles upgrade edges correctly", func() {
			By("creating a valid Operator resource")
			operator.Spec = operatorv1alpha1.OperatorSpec{
				PackageName: "prometheus",
				Version:     "0.37.0",
			}
			Expect(c.Create(ctx, operator)).To(Succeed())
			By("eventually reporting a successful resolution")
			Eventually(func(g Gomega) {
				g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
				g.Expect(cond.Message).To(ContainSubstring("resolved to"))
				g.Expect(operator.Status.ResolvedBundleResource).ToNot(BeEmpty())
			}).Should(Succeed())

			By("updating the Operator resource to a non-successor version")
			operator.Spec.Version = "0.65.1" // current (0.37.0) and successor (0.47.0) are the only values that would be SAT.
			Expect(c.Update(ctx, operator)).To(Succeed())
			By("eventually reporting an unsatisfiable resolution")
			Eventually(func(g Gomega) {
				g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonResolutionFailed))
				g.Expect(cond.Message).To(MatchRegexp(`^constraints not satisfiable:.*; installed package prometheus requires at least one of.*0.47.0[^,]*,[^,]*0.37.0[^;]*;.*`))
				g.Expect(operator.Status.ResolvedBundleResource).To(BeEmpty())
			}).Should(Succeed())

			By("updating the Operator resource to a valid upgrade edge")
			operator.Spec.Version = "0.47.0"
			Expect(c.Update(ctx, operator)).To(Succeed())
			By("eventually reporting a successful resolution and bundle path")
			Eventually(func(g Gomega) {
				g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
				g.Expect(cond.Message).To(ContainSubstring("resolved to"))
				g.Expect(operator.Status.ResolvedBundleResource).ToNot(BeEmpty())
			}).Should(Succeed())
		})

		AfterEach(func() {
			Expect(c.Delete(ctx, operator)).To(Succeed())
			Eventually(func(g Gomega) {
				err := c.Get(ctx, types.NamespacedName{Name: operator.Name}, &operatorv1alpha1.Operator{})
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).Should(Succeed())
		})

	})
})
