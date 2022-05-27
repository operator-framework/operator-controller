package e2e

import (
	"context"
	"fmt"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	platformv1alpha1 "github.com/timflannagan/platform-operators/api/v1alpha1"
)

const (
	dataBaseDir = "testdata"
)

var _ = Describe("platform operators controller", func() {
	var (
		ns  *corev1.Namespace
		ctx context.Context
	)
	BeforeEach(func() {
		ctx = context.Background()
		ns = SetupTestNamespace(c, genName("e2e-"))
	})
	AfterEach(func() {
		Expect(c.Delete(ctx, ns)).To(BeNil())
	})
	// TODO(tflannag): We _really_ need to fix OLM's registry connection logic as waiting on
	// the CatalogSource resource to report a "READY" connection state accounts for the majority
	// of the overall e2e suite time, despite the underlying registry Pod serving the connection
	// already for 20/30s sometimes.
	When("sourcing content from a single catalogsource", func() {
		var (
			catalog MagicCatalog
		)
		BeforeEach(func() {
			provider, err := NewFileBasedFiledBasedCatalogProvider(filepath.Join(dataBaseDir, "prometheus.yaml"))
			Expect(err).To(BeNil())

			catalog = NewMagicCatalog(c, ns.GetName(), "prometheus", provider)
			Expect(catalog.DeployCatalog(ctx)).To(BeNil())
		})
		AfterEach(func() {
			Expect(catalog.UndeployCatalog(ctx)).To(BeNil())
		})

		When("a platformoperator has been created", func() {
			var (
				po *platformv1alpha1.PlatformOperator
			)
			BeforeEach(func() {
				po = &platformv1alpha1.PlatformOperator{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "prometheus-operator",
					},
					Spec: platformv1alpha1.PlatformOperatorSpec{
						PackageName: "prometheus-operator",
					},
				}
				Expect(c.Create(ctx, po)).To(BeNil())
			})
			AfterEach(func() {
				Expect(c.Delete(ctx, po)).To(BeNil())
			})
			It("should generate a bundleinstance with a metadata.Name that matches the platformoperator's metadata.Name", func() {
				Eventually(func() error {
					bi := &rukpakv1alpha1.BundleInstance{}
					return c.Get(ctx, types.NamespacedName{Name: po.GetName()}, bi)
				}).Should(Succeed())
			})
			It("should generate a bundleinstance that contains the different unique provisioner ID", func() {
				Eventually(func() bool {
					bi := &rukpakv1alpha1.BundleInstance{}
					if err := c.Get(ctx, types.NamespacedName{Name: po.GetName()}, bi); err != nil {
						return false
					}
					return bi.Spec.Template.Spec.ProvisionerClassName != bi.Spec.ProvisionerClassName
				}).Should(BeTrue())
			})
			It("should choose the highest olm.bundle semver available in the catalog", func() {
				Eventually(func() bool {
					bi := &rukpakv1alpha1.BundleInstance{}
					if err := c.Get(ctx, types.NamespacedName{Name: po.GetName()}, bi); err != nil {
						return false
					}
					return bi.Spec.Template.Spec.Source.Image.Ref == "quay.io/operatorhubio/prometheus@sha256:5b04c49d8d3eff6a338b56ec90bdf491d501fe301c9cdfb740e5bff6769a21ed"
				}).Should(BeTrue())
			})
			It("should result in a successful installation", func() {
				Eventually(func() (*metav1.Condition, error) {
					bi := &rukpakv1alpha1.BundleInstance{}
					if err := c.Get(ctx, types.NamespacedName{Name: po.GetName()}, bi); err != nil {
						return nil, err
					}
					if bi.Status.InstalledBundleName == "" {
						return nil, fmt.Errorf("waiting for bundle name to be populated")
					}
					return meta.FindStatusCondition(bi.Status.Conditions, rukpakv1alpha1.TypeInstalled), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(rukpakv1alpha1.TypeInstalled)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonInstallationSucceeded)),
					WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("instantiated bundle")),
				))
			})
		})
	})
})
