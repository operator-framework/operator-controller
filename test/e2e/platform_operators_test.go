package e2e

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/timflannagan/platform-operators/api/v1alpha1"
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
			provider, err := NewFileBasedFiledBasedCatalogProvider(filepath.Join(dataBaseDir, "prometheus.v0.1.0.yaml"))
			Expect(err).To(BeNil())

			catalog = NewMagicCatalog(c, ns.GetName(), "prometheus", provider)
			Expect(catalog.DeployCatalog(ctx)).To(BeNil())
		})
		AfterEach(func() {
			Expect(catalog.UndeployCatalog(ctx)).To(BeNil())
		})
		When("a platformoperator is targeting an invalid package name", func() {
			var (
				po *platformv1alpha1.PlatformOperator
			)
			BeforeEach(func() {
				po = &platformv1alpha1.PlatformOperator{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "prometheus-operator-invalid",
					},
					Spec: platformv1alpha1.PlatformOperatorSpec{
						Packages: []string{"prometheus-operator-invalid"},
					},
				}
				Expect(c.Create(ctx, po)).To(BeNil())
			})
			AfterEach(func() {
				Expect(c.Delete(ctx, po)).To(BeNil())
			})
			It("should bubble up the resolution failures", func() {
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(po), po); err != nil {
						return nil, err
					}
					return meta.FindStatusCondition(po.Status.Conditions, platformv1alpha1.TypeSourced), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(platformv1alpha1.TypeSourced)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionFalse)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal("SolverProblemFailed")),
					WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("constraints not satisfiable")),
				))
			})
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
						Packages: []string{"prometheus-operator"},
					},
				}
				Expect(c.Create(ctx, po)).To(BeNil())
			})
			AfterEach(func() {
				Expect(c.Delete(ctx, po)).To(BeNil())
			})
			It("should result in packages being successfully sourced", func() {
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(po), po); err != nil {
						return nil, err
					}
					return meta.FindStatusCondition(po.Status.Conditions, platformv1alpha1.TypeSourced), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(platformv1alpha1.TypeSourced)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(platformv1alpha1.ReasonSourceSuccessful)),
					WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("Successfully sourced package candidates")),
				))
			})
		})
	})
})
