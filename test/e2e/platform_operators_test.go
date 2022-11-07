package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/openshift/api/platform/v1alpha1"
	platformtypes "github.com/operator-framework/operator-controller/api/v1alpha1"
)

var _ = Describe("platform operators controller", func() {
	var (
		ctx context.Context
	)
	BeforeEach(func() {
		ctx = context.Background()

		Skip("FIXME: Skipping tests while phase 0 requires usage of redhat-operators downstream catalog")
	})

	When("a valid platformoperators has been created", func() {
		var (
			po *platformv1alpha1.PlatformOperator
		)
		BeforeEach(func() {
			po = &platformv1alpha1.PlatformOperator{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "cert-manager",
				},
				Spec: platformv1alpha1.PlatformOperatorSpec{
					Package: platformv1alpha1.Package{
						Name: "openshift-cert-manager-operator",
					},
				},
			}
			Expect(c.Create(ctx, po)).To(BeNil())
		})
		AfterEach(func() {
			Expect(HandleTestCaseFailure()).To(BeNil())
			Expect(c.Delete(ctx, po, client.PropagationPolicy(metav1.DeletePropagationForeground))).To(BeNil())
		})
		It("should generate a Bundle Deployment with a metadata.Name that matches the platformoperator's metadata.Name", func() {
			Eventually(func() error {
				bi := &rukpakv1alpha1.BundleDeployment{}
				return c.Get(ctx, types.NamespacedName{Name: po.GetName()}, bi)
			}).Should(Succeed())
		})
		It("should generate a Bundle Deployment that contains the different unique provisioner ID", func() {
			Eventually(func() bool {
				bi := &rukpakv1alpha1.BundleDeployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: po.GetName()}, bi); err != nil {
					return false
				}
				return bi.Spec.Template.Spec.ProvisionerClassName != bi.Spec.ProvisionerClassName
			}).Should(BeTrue())
		})
		It("should choose the right registry+v1 bundle image", func() {
			Eventually(func() string {
				bi := &rukpakv1alpha1.BundleDeployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: po.GetName()}, bi); err != nil {
					return ""
				}
				return bi.Spec.Template.Spec.Source.Image.Ref
			}).Should(ContainSubstring("registry.redhat.io/cert-manager/cert-manager-operator-bundle"))
		})
		It("should result in a successful BD status", func() {
			Eventually(func() (*metav1.Condition, error) {
				bi := &rukpakv1alpha1.BundleDeployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: po.GetName()}, bi); err != nil {
					return nil, err
				}
				if bi.Status.ActiveBundle == "" {
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
		It("should result in the successful BD status bubbling up the PO resource", func() {
			Eventually(func() (*metav1.Condition, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(po), po); err != nil {
					return nil, err
				}
				return meta.FindStatusCondition(po.Status.Conditions, platformtypes.TypeInstalled), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(platformtypes.TypeInstalled)),
				WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
				WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(platformtypes.ReasonInstallSuccessful)),
				WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("Successfully applied")),
			))
		})
		It("should result in the successful in a non-empty status.activeBundleDeployment field in the PO resource", func() {
			Eventually(func() (bool, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(po), po); err != nil {
					return false, err
				}
				return po.Status.ActiveBundleDeployment.Name == po.GetName(), nil
			}).Should(BeTrue())
		})

		// Note: this is a known limitation in the current implementation.
		// See https://github.com/operator-framework/operator-controller/issues/47 for more details.
		It("the underlying BD has been modified", func() {
			Eventually(func() error {
				bi := &rukpakv1alpha1.BundleDeployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: po.GetName()}, bi); err != nil {
					return err
				}
				bi.Spec.Template.Spec.Source.Image.Ref = "quay.io/openshift/origin-cluster-platform-operators-manager:latest"

				return c.Update(ctx, bi)
			}).Should(Succeed())

			Consistently(func() bool {
				bi := &rukpakv1alpha1.BundleDeployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: po.GetName()}, bi); err != nil {
					return false
				}
				return bi.Spec.Template.Spec.Source.Image.Ref == "quay.io/openshift/origin-cluster-platform-operators-manager:latest"
			}).Should(BeTrue())
		})
	})
	When("an invalid PO that references a non-existent package has been created", func() {
		var (
			po *platformv1alpha1.PlatformOperator
		)
		BeforeEach(func() {
			po = &platformv1alpha1.PlatformOperator{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "non-existent-operator",
				},
				Spec: platformv1alpha1.PlatformOperatorSpec{
					Package: platformv1alpha1.Package{
						Name: "non-existent-operator",
					},
				},
			}
			Expect(c.Create(ctx, po)).To(BeNil())
		})
		AfterEach(func() {
			Expect(HandleTestCaseFailure()).To(BeNil())
			Expect(c.Delete(ctx, po, client.PropagationPolicy(metav1.DeletePropagationForeground))).To(BeNil())
		})

		It("should eventually result in a failed attempt at sourcing that non-existent package", func() {
			Eventually(func() (*metav1.Condition, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(po), po); err != nil {
					return nil, err
				}
				return meta.FindStatusCondition(po.Status.Conditions, platformtypes.TypeInstalled), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(platformtypes.TypeInstalled)),
				WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionFalse)),
				WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(platformtypes.ReasonSourceFailed)),
				WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("failed to find candidate")),
			))
		})
	})
	When("an invalid PO has been created", func() {
		var (
			po *platformv1alpha1.PlatformOperator
		)
		BeforeEach(func() {
			po = &platformv1alpha1.PlatformOperator{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "cincinnati-operator",
				},
				Spec: platformv1alpha1.PlatformOperatorSpec{
					Package: platformv1alpha1.Package{
						Name: "cincinnati-operator",
					},
				},
			}
			Expect(c.Create(ctx, po)).To(BeNil())
		})
		AfterEach(func() {
			Expect(HandleTestCaseFailure()).To(BeNil())
			Expect(c.Delete(ctx, po, client.PropagationPolicy(metav1.DeletePropagationForeground))).To(BeNil())
		})

		It("should eventually result in a failed attempt at applying the unpacked contents", func() {
			Eventually(func() (*metav1.Condition, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(po), po); err != nil {
					return nil, err
				}
				return meta.FindStatusCondition(po.Status.Conditions, platformtypes.TypeInstalled), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(platformtypes.TypeInstalled)),
				WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionFalse)),
				WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(rukpakv1alpha1.ReasonUnpackFailed)),
				WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("convert registry+v1 bundle to plain+v0 bundle: AllNamespace install mode must be enabled")),
			))
		})
	})
})
