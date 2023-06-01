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
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultTimeout = 30 * time.Second
	defaultPoll    = 1 * time.Second
)

var _ = Describe("Operator Install", func() {
	var (
		ctx             context.Context
		operatorCatalog *catalogd.Catalog
		operator        *operatorv1alpha1.Operator
	)
	When("An operator is installed from an operator catalog", func() {
		BeforeEach(func() {
			ctx = context.Background()
			operatorCatalog = &catalogd.Catalog{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("test-catalog-%s", rand.String(5)),
				},
				Spec: catalogd.CatalogSpec{
					Source: catalogd.CatalogSource{
						Type: catalogd.SourceTypeImage,
						Image: &catalogd.ImageSource{
							// (TODO): Set up a local image registry, and build and store a test catalog in it
							// to use in the test suite
							Ref: "quay.io/olmtest/e2e-index:single-package-fbc",
						},
					},
				},
			}
			Expect(c.Create(ctx, operatorCatalog)).To(Succeed())
			Eventually(checkCatalogUnpacked(ctx, operatorCatalog)).WithTimeout(5 * time.Minute).WithPolling(defaultPoll).Should(BeNil())

			By("creating the Operator resource")
			pkgName := "argocd-operator"
			operatorName := fmt.Sprintf("%s-%s", pkgName, rand.String(8))
			operator = &operatorv1alpha1.Operator{
				ObjectMeta: metav1.ObjectMeta{
					Name: operatorName,
				},
				Spec: operatorv1alpha1.OperatorSpec{
					PackageName: pkgName,
					Version:     "0.6.0",
				},
			}
			Expect(c.Create(ctx, operator)).To(Succeed())
		})
		It("installs the specified package with correct bundle path", func() {
			By("eventually reporting a successful resolution")
			Eventually(checkOperatorResolved(ctx, operator)).WithTimeout(defaultTimeout).WithPolling(defaultPoll).Should(Succeed())

			By("eventually reporting a successful installation")
			Eventually(checkOperatorInstalled(ctx, operator)).WithTimeout(defaultTimeout).WithPolling(defaultPoll).Should(Succeed())

			By("verifying the expected bundle path")
			bd := rukpakv1alpha1.BundleDeployment{}
			Expect(c.Get(ctx, client.ObjectKeyFromObject(operator), &bd)).To(Succeed())
			Expect(bd.Spec.Template.Spec.Source.Type).To(Equal(rukpakv1alpha1.SourceTypeImage))
			Expect(bd.Spec.Template.Spec.Source.Image.Ref).To(Equal("quay.io/operatorhubio/argocd-operator@sha256:1a9b3c8072f2d7f4d6528fa32905634d97b7b4c239ef9887e3fb821ff033fef6"))
			Expect(operator.Status.ResolvedBundleResource).To(Equal(bd.Spec.Template.Spec.Source.Image.Ref))
			Expect(operator.Status.InstalledBundleResource).To(Equal(bd.Spec.Template.Spec.Source.Image.Ref))
		})
		AfterEach(func() {
			Expect(c.Delete(ctx, operator)).To(Succeed())
			Expect(c.Delete(ctx, operatorCatalog)).To(Succeed())
		})
	})
})

func checkCatalogUnpacked(ctx context.Context, catalog *catalogd.Catalog) func() error {
	return func() error {
		return checkCondition(ctx, catalog, catalogd.TypeUnpacked,
			statusReason{metav1.ConditionTrue, catalogd.ReasonUnpackSuccessful},
			statusReason{metav1.ConditionFalse, catalogd.ReasonUnpackFailed},
		)
	}
}

func checkOperatorResolved(ctx context.Context, op *operatorv1alpha1.Operator) func() error {
	return func() error {
		return checkCondition(ctx, op, operatorv1alpha1.TypeResolved,
			statusReason{metav1.ConditionTrue, operatorv1alpha1.ReasonSuccess},
			statusReason{metav1.ConditionFalse, operatorv1alpha1.ReasonResolutionFailed},
		)
	}
}

func checkOperatorInstalled(ctx context.Context, op *operatorv1alpha1.Operator) func() error {
	return func() error {
		return checkCondition(ctx, op, operatorv1alpha1.TypeInstalled,
			statusReason{metav1.ConditionTrue, operatorv1alpha1.ReasonSuccess},
			statusReason{metav1.ConditionFalse, operatorv1alpha1.ReasonInstallationFailed},
		)
	}
}

type statusReason struct {
	status metav1.ConditionStatus
	reason string
}

func checkCondition(ctx context.Context, obj client.Object, condType string, successStatusReason statusReason, failureStatusReasons ...statusReason) error {
	if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		return err
	}

	var conditions []metav1.Condition
	switch v := obj.(type) {
	case *catalogd.Catalog:
		conditions = v.Status.Conditions
	case *operatorv1alpha1.Operator:
		conditions = v.Status.Conditions
	default:
		return StopTrying(fmt.Sprintf("cannot get conditions for unknown object type %T", obj))
	}

	cond := apimeta.FindStatusCondition(conditions, condType)
	if cond == nil {
		return fmt.Errorf("condition %q is not set; status: %#v", condType, conditions)
	}
	if successStatusReason == (statusReason{cond.Status, cond.Reason}) {
		return nil
	}

	err := fmt.Errorf("condition %q is %s/%s; message: %q", cond.Type, cond.Status, cond.Reason, cond.Message)
	for _, failureStatusReason := range failureStatusReasons {
		if cond.Status == failureStatusReason.status && cond.Reason == failureStatusReason.reason {
			return StopTrying(err.Error())
		}
	}
	return err
}
