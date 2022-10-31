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
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformtypes "github.com/timflannagan/platform-operators/api/v1alpha1"
)

var _ = Describe("operators controller", func() {
	var (
		ctx context.Context
		ns  *corev1.Namespace
	)
	BeforeEach(func() {
		ctx = context.Background()
		ns = SetupTestNamespace(c, genName("e2e-"))
	})
	AfterEach(func() {
		Expect(c.Delete(ctx, ns)).To(BeNil())
	})

	When("sourcing content from a single catalog source", func() {
		var (
			catalog MagicCatalog
		)
		BeforeEach(func() {
			provider, err := NewFileBasedFiledBasedCatalogProvider(filepath.Join(dataBaseDir, "prometheus.v0.2.0.yaml"))
			Expect(err).To(BeNil())

			catalog = NewMagicCatalog(c, ns.GetName(), "prometheus", provider)
			Expect(catalog.DeployCatalog(ctx)).To(BeNil())
		})
		AfterEach(func() {
			Expect(catalog.UndeployCatalog(ctx)).To(BeNil())
		})

		When("an operator has been created with an explicit version specified", func() {
			var (
				o *platformtypes.Operator
			)
			BeforeEach(func() {
				o = &platformtypes.Operator{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "valid-",
					},
					Spec: platformtypes.OperatorSpec{
						Catalog: platformtypes.CatalogSpec{
							Name:      "prometheus",
							Namespace: ns.GetName(),
						},
						Package: platformtypes.PackageSpec{
							Name:    "prometheus-operator",
							Version: "0.1.0",
						},
					},
				}
				Expect(c.Create(ctx, o)).To(Succeed())
			})
			AfterEach(func() {
				Expect(c.Delete(ctx, o)).To(Succeed())
			})

			It("should eventually result in a successful installation", func() {
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return nil, err
					}
					if o.Status.ActiveBundleDeployment.Name == "" {
						return nil, fmt.Errorf("waiting for bundledeployment name to be populated")
					}
					return meta.FindStatusCondition(o.Status.Conditions, platformtypes.TypeInstalled), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(platformtypes.TypeInstalled)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(platformtypes.ReasonInstallSuccessful)),
				))
			})

			It("should generate a BundleDeployment that matches the expected version", func() {
				Eventually(func() bool {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return false
					}
					bdName := o.Status.ActiveBundleDeployment.Name
					if bdName == "" {
						return false
					}

					bd := &rukpakv1alpha1.BundleDeployment{}
					if err := c.Get(ctx, types.NamespacedName{Name: bdName}, bd); err != nil {
						return false
					}
					// Note: this points to the v0.1.0 image.
					return bd.Spec.Template.Spec.Source.Image.Ref == "quay.io/operatorhubio/prometheus:v0.47.0"
				}).Should(BeTrue())
			})
		})

		When("an operator has been created with a version range specified", func() {
			var (
				o *platformtypes.Operator
			)
			BeforeEach(func() {
				o = &platformtypes.Operator{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "valid-",
					},
					Spec: platformtypes.OperatorSpec{
						Catalog: platformtypes.CatalogSpec{
							Name:      "prometheus",
							Namespace: ns.GetName(),
						},
						Package: platformtypes.PackageSpec{
							Name:    "prometheus-operator",
							Version: ">0.1.0",
						},
					},
				}
				Expect(c.Create(ctx, o)).To(Succeed())
			})
			AfterEach(func() {
				Expect(c.Delete(ctx, o)).To(Succeed())
			})

			It("should eventually result in a successful installation", func() {
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return nil, err
					}
					if o.Status.ActiveBundleDeployment.Name == "" {
						return nil, fmt.Errorf("waiting for bundledeployment name to be populated")
					}
					return meta.FindStatusCondition(o.Status.Conditions, platformtypes.TypeInstalled), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(platformtypes.TypeInstalled)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(platformtypes.ReasonInstallSuccessful)),
				))
			})

			It("should generate a BundleDeployment that matches the expected version", func() {
				Eventually(func() bool {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return false
					}
					bdName := o.Status.ActiveBundleDeployment.Name
					if bdName == "" {
						return false
					}

					bd := &rukpakv1alpha1.BundleDeployment{}
					if err := c.Get(ctx, types.NamespacedName{Name: bdName}, bd); err != nil {
						return false
					}
					// Note: this points to the v0.2.0 image.
					return bd.Spec.Template.Spec.Source.Image.Ref == "quay.io/operatorhubio/prometheus:v0.47.0--20220413T184225"
				}).Should(BeTrue())
			})
		})

		When("a valid operator is created", func() {
			var (
				o *platformtypes.Operator
			)
			BeforeEach(func() {
				o = &platformtypes.Operator{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "valid-",
					},
					Spec: platformtypes.OperatorSpec{
						Catalog: &platformtypes.CatalogSpec{
							Name:      "prometheus",
							Namespace: ns.GetName(),
						},
						Package: &platformtypes.PackageSpec{
							Name: "prometheus-operator",
						},
					},
				}
				Expect(c.Create(ctx, o)).To(Succeed())
			})
			AfterEach(func() {
				Expect(c.Delete(ctx, o)).To(Succeed())
			})

			It("should eventually contain a non-empty status.ActiveBundleDeployment.Name", func() {
				Eventually(func() (bool, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return false, err
					}
					return o.Status.ActiveBundleDeployment.Name != "", nil
				})
			})

			It("should eventually result in a successful installation", func() {
				Eventually(func() (*metav1.Condition, error) {
					if err := c.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
						return nil, err
					}
					if o.Status.ActiveBundleDeployment.Name == "" {
						return nil, fmt.Errorf("waiting for bundledeployment name to be populated")
					}
					return meta.FindStatusCondition(o.Status.Conditions, platformtypes.TypeInstalled), nil
				}).Should(And(
					Not(BeNil()),
					WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(platformtypes.TypeInstalled)),
					WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
					WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(platformtypes.ReasonInstallSuccessful)),
				))
			})
		})
	})
})
