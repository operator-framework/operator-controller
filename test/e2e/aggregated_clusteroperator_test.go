package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/openshift/api/platform/v1alpha1"
	platformtypes "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/clusteroperator"
	"github.com/operator-framework/operator-controller/internal/util"
)

var _ = Describe("aggregated clusteroperator controller", func() {
	var (
		ctx context.Context
	)
	BeforeEach(func() {
		ctx = context.Background()

		supported, err := util.IsAPIAvailable(dc, schema.GroupVersion{
			Group:   "config.openshift.io",
			Version: "v1",
		})
		if err != nil || supported != true {
			Skip("ClusterOperator GK doesn't exist on the cluster")
		}
	})

	When("no POs have been installed on the cluster", func() {
		AfterEach(func() {
			Expect(HandleTestCaseFailure()).To(BeNil())
		})
		It("should consistently report a healthy CO status back to the CVO", func() {
			Consistently(func() (*configv1.ClusterOperatorStatusCondition, error) {
				co := &configv1.ClusterOperator{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusteroperator.AggregateResourceName}, co); err != nil {
					return nil, err
				}
				return FindStatusCondition(co.Status.Conditions, configv1.OperatorAvailable), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) configv1.ClusterStatusConditionType { return c.Type }, Equal(configv1.OperatorAvailable)),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) configv1.ConditionStatus { return c.Status }, Equal(configv1.ConditionTrue)),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) string { return c.Reason }, Equal(clusteroperator.ReasonAsExpected)),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) string { return c.Message }, ContainSubstring("No platform operators are present in the cluster")),
			))
		})
		It("should consistently contain a populated status.versions array", func() {
			Consistently(func() (bool, error) {
				co := &configv1.ClusterOperator{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusteroperator.AggregateResourceName}, co); err != nil {
					return false, err
				}
				if len(co.Status.Versions) != 1 {
					return false, nil
				}
				version := co.Status.Versions[0]

				return version.Name != "" && version.Version != "", nil
			}).Should(BeTrue())
		})
		It("should consistently contain a populated status.relatedObjects array", func() {
			Consistently(func() (bool, error) {
				co := &configv1.ClusterOperator{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusteroperator.AggregateResourceName}, co); err != nil {
					return false, err
				}
				return len(co.Status.RelatedObjects) == 4, nil
			}).Should(BeTrue())
		})
	})

	When("installing a series of POs successfully", func() {
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

		It("should eventually result in a successful application", func() {
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

		It("should eventually report a healthy CO status back to the CVO", func() {
			Eventually(func() (*configv1.ClusterOperatorStatusCondition, error) {
				co := &configv1.ClusterOperator{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusteroperator.AggregateResourceName}, co); err != nil {
					return nil, err
				}
				return FindStatusCondition(co.Status.Conditions, configv1.OperatorAvailable), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) configv1.ClusterStatusConditionType { return c.Type }, Equal(configv1.OperatorAvailable)),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) configv1.ConditionStatus { return c.Status }, Equal(configv1.ConditionTrue)),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) string { return c.Reason }, Equal(clusteroperator.ReasonAsExpected)),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) string { return c.Message }, ContainSubstring("All platform operators are in a successful state")),
			))
		})

		It("should consistently contain a populated status.versions", func() {
			Consistently(func() (bool, error) {
				co := &configv1.ClusterOperator{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusteroperator.AggregateResourceName}, co); err != nil {
					return false, err
				}
				if len(co.Status.Versions) != 1 {
					return false, nil
				}
				version := co.Status.Versions[0]

				return version.Name != "" && version.Version != "", nil
			}).Should(BeTrue())
		})
	})

	When("a failing PO has been encountered", func() {
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

		It("should eventually report an unvailable CO status back to the CVO", func() {
			Eventually(func() (*configv1.ClusterOperatorStatusCondition, error) {
				co := &configv1.ClusterOperator{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusteroperator.AggregateResourceName}, co); err != nil {
					return nil, err
				}
				return FindStatusCondition(co.Status.Conditions, configv1.OperatorAvailable), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) configv1.ClusterStatusConditionType { return c.Type }, Equal(configv1.OperatorAvailable)),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) configv1.ConditionStatus { return c.Status }, Equal(configv1.ConditionFalse)),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) string { return c.Reason }, Equal(clusteroperator.ReasonPlatformOperatorError)),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) string { return c.Message }, ContainSubstring("encountered the failing")),
			))
		})
	})

	When("there's a mixture of failing and successful POs deployed on the cluster", func() {
		var (
			invalid *platformv1alpha1.PlatformOperator
			valid   *platformv1alpha1.PlatformOperator
		)
		BeforeEach(func() {
			invalid = &platformv1alpha1.PlatformOperator{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "non-existent-operator",
				},
				Spec: platformv1alpha1.PlatformOperatorSpec{
					Package: platformv1alpha1.Package{
						Name: "non-existent-operator",
					},
				},
			}
			Expect(c.Create(ctx, invalid)).To(BeNil())

			valid = &platformv1alpha1.PlatformOperator{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "cert-manager",
				},
				Spec: platformv1alpha1.PlatformOperatorSpec{
					Package: platformv1alpha1.Package{
						Name: "openshift-cert-manager-operator",
					},
				},
			}
			Expect(c.Create(ctx, valid)).To(BeNil())
		})
		AfterEach(func() {
			Expect(HandleTestCaseFailure()).To(BeNil())
			Expect(c.Delete(ctx, invalid, client.PropagationPolicy(metav1.DeletePropagationForeground))).To(BeNil())
			Expect(c.Delete(ctx, valid, client.PropagationPolicy(metav1.DeletePropagationForeground))).To(BeNil())
		})

		It("should eventually result in a failed attempt at sourcing that non-existent package", func() {
			Eventually(func() (*metav1.Condition, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(invalid), invalid); err != nil {
					return nil, err
				}
				return meta.FindStatusCondition(invalid.Status.Conditions, platformtypes.TypeInstalled), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(platformtypes.TypeInstalled)),
				WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionFalse)),
				WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(platformtypes.ReasonSourceFailed)),
				WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("failed to find candidate")),
			))
		})

		It("should eventually result in a successful application", func() {
			Eventually(func() (*metav1.Condition, error) {
				if err := c.Get(ctx, client.ObjectKeyFromObject(valid), valid); err != nil {
					return nil, err
				}
				return meta.FindStatusCondition(valid.Status.Conditions, platformtypes.TypeInstalled), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *metav1.Condition) string { return c.Type }, Equal(platformtypes.TypeInstalled)),
				WithTransform(func(c *metav1.Condition) metav1.ConditionStatus { return c.Status }, Equal(metav1.ConditionTrue)),
				WithTransform(func(c *metav1.Condition) string { return c.Reason }, Equal(platformtypes.ReasonInstallSuccessful)),
				WithTransform(func(c *metav1.Condition) string { return c.Message }, ContainSubstring("Successfully applied")),
			))
		})

		It("should eventually report an unvailable CO status back to the CVO", func() {
			Eventually(func() (*configv1.ClusterOperatorStatusCondition, error) {
				co := &configv1.ClusterOperator{}
				if err := c.Get(ctx, types.NamespacedName{Name: clusteroperator.AggregateResourceName}, co); err != nil {
					return nil, err
				}
				return FindStatusCondition(co.Status.Conditions, configv1.OperatorAvailable), nil
			}).Should(And(
				Not(BeNil()),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) configv1.ClusterStatusConditionType { return c.Type }, Equal(configv1.OperatorAvailable)),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) configv1.ConditionStatus { return c.Status }, Equal(configv1.ConditionFalse)),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) string { return c.Reason }, Equal(clusteroperator.ReasonPlatformOperatorError)),
				WithTransform(func(c *configv1.ClusterOperatorStatusCondition) string { return c.Message }, ContainSubstring("encountered the failing")),
			))
		})
	})
})

// FindStatusCondition finds the conditionType in conditions.
// Note: manually vendored from o/library-go/pkg/config/clusteroperator/v1helpers/status.go.
func FindStatusCondition(conditions []configv1.ClusterOperatorStatusCondition, conditionType configv1.ClusterStatusConditionType) *configv1.ClusterOperatorStatusCondition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
