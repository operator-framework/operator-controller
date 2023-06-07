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
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
)

const (
	defaultTimeout  = 30 * time.Second
	defaultPoll     = 1 * time.Second
	testCatalogRef  = "localhost/testdata/catalogs/test-catalog:e2e"
	testCatalogName = "test-catalog"
)

var _ = Describe("Operator Install", func() {
	var (
		ctx             context.Context
		pkgName         string
		operatorName    string
		operator        *operatorv1alpha1.Operator
		operatorCatalog *catalogd.Catalog
	)
	When("An operator is installed from an operator catalog", func() {
		BeforeEach(func() {
			ctx = context.Background()
			pkgName = "prometheus"
			operatorName = fmt.Sprintf("operator-%s", rand.String(8))
			operator = &operatorv1alpha1.Operator{
				ObjectMeta: metav1.ObjectMeta{
					Name: operatorName,
				},
				Spec: operatorv1alpha1.OperatorSpec{
					PackageName: pkgName,
				},
			}
			operatorCatalog = &catalogd.Catalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: testCatalogName,
				},
				Spec: catalogd.CatalogSpec{
					Source: catalogd.CatalogSource{
						Type: catalogd.SourceTypeImage,
						Image: &catalogd.ImageSource{
							Ref: testCatalogRef,
						},
					},
				},
			}
		})
		It("resolves the specified package with correct bundle path", func() {
			err := c.Create(ctx, operatorCatalog)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: testCatalogName}, operatorCatalog)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(operatorCatalog.Status.Conditions)).To(Equal(1))
				cond := apimeta.FindStatusCondition(operatorCatalog.Status.Conditions, catalogd.TypeUnpacked)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(catalogd.ReasonUnpackSuccessful))
				g.Expect(cond.Message).To(ContainSubstring("successfully unpacked the catalog image"))
			}).WithTimeout(defaultTimeout).WithPolling(defaultPoll).Should(Succeed())

			By("creating the Operator resource")
			err = c.Create(ctx, operator)
			Expect(err).ToNot(HaveOccurred())

			By("eventually reporting a successful resolution and bundle path")
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)
				g.Expect(err).ToNot(HaveOccurred())

				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
				g.Expect(cond.Message).To(ContainSubstring("resolved to"))
				g.Expect(operator.Status.ResolvedBundleResource).ToNot(BeEmpty())
			}).WithTimeout(defaultTimeout).WithPolling(defaultPoll).Should(Succeed())

			By("eventually installing the package successfully")
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)
				g.Expect(err).ToNot(HaveOccurred())
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeInstalled)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
				g.Expect(cond.Message).To(ContainSubstring("installed from"))
				g.Expect(operator.Status.InstalledBundleResource).ToNot(BeEmpty())
				bd := rukpakv1alpha1.BundleDeployment{}
				err = c.Get(ctx, types.NamespacedName{Name: operatorName}, &bd)
				g.Expect(err).ToNot(HaveOccurred())

				cond = apimeta.FindStatusCondition(bd.Status.Conditions, rukpakv1alpha1.TypeHasValidBundle)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(rukpakv1alpha1.ReasonUnpackSuccessful))

				cond = apimeta.FindStatusCondition(bd.Status.Conditions, rukpakv1alpha1.TypeInstalled)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(rukpakv1alpha1.ReasonInstallationSucceeded))
			}).WithTimeout(defaultTimeout).WithPolling(defaultPoll).Should(Succeed())
		})
		It("resolves again when a new catalog is available", func() {
			Eventually(func(g Gomega) {
				// target package should not be present on cluster
				err := c.Get(ctx, types.NamespacedName{Name: pkgName}, &catalogd.Package{})
				Expect(errors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(5 * time.Minute).WithPolling(defaultPoll).Should(Succeed())

			By("creating the Operator resource")
			err := c.Create(ctx, operator)
			Expect(err).ToNot(HaveOccurred())

			By("failing to find Operator during resolution")
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)
				g.Expect(err).ToNot(HaveOccurred())
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonResolutionFailed))
				g.Expect(cond.Message).To(Equal(fmt.Sprintf("package '%s' not found", pkgName)))
			}).WithTimeout(defaultTimeout).WithPolling(defaultPoll).Should(Succeed())

			By("creating an Operator catalog with the desired package")
			err = c.Create(ctx, operatorCatalog)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: operatorCatalog.Name}, operatorCatalog)
				g.Expect(err).ToNot(HaveOccurred())
				cond := apimeta.FindStatusCondition(operatorCatalog.Status.Conditions, catalogd.TypeUnpacked)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(catalogd.ReasonUnpackSuccessful))
			}).WithTimeout(5 * time.Minute).WithPolling(defaultPoll).Should(Succeed())

			By("eventually resolving the package successfully")
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)
				g.Expect(err).ToNot(HaveOccurred())
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorv1alpha1.TypeResolved)
				g.Expect(cond).ToNot(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal(operatorv1alpha1.ReasonSuccess))
			}).WithTimeout(defaultTimeout).WithPolling(defaultPoll).Should(Succeed())
		})
		AfterEach(func() {
			err := c.Delete(ctx, operator)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: operatorName}, &operatorv1alpha1.Operator{})
				Expect(errors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(defaultTimeout).WithPolling(defaultPoll).Should(Succeed())

			err = c.Delete(ctx, operatorCatalog)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				err = c.Get(ctx, types.NamespacedName{Name: operatorCatalog.Name}, &catalogd.Catalog{})
				Expect(errors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(defaultTimeout).WithPolling(defaultPoll).Should(Succeed())

			// speed up delete without waiting for gc
			err = c.DeleteAllOf(ctx, &catalogd.BundleMetadata{})
			Expect(err).ToNot(HaveOccurred())
			err = c.DeleteAllOf(ctx, &catalogd.Package{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func(g Gomega) {
				// ensure resource cleanup
				packages := &catalogd.PackageList{}
				err = c.List(ctx, packages)
				Expect(err).To(BeNil())
				Expect(packages.Items).To(BeEmpty())

				bmd := &catalogd.BundleMetadataList{}
				err = c.List(ctx, bmd)
				Expect(err).To(BeNil())
				Expect(bmd.Items).To(BeEmpty())

				err = c.Get(ctx, types.NamespacedName{Name: operatorName}, &rukpakv1alpha1.BundleDeployment{})
				Expect(errors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(5 * time.Minute).WithPolling(defaultPoll).Should(Succeed())
		})
	})
})
