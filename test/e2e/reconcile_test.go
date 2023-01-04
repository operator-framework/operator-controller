package e2e

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

var _ = Describe("Basic Reconciliation", func() {
	When("an Operator CR is created", func() {
		var (
			bundle *operatorv1alpha1.Operator
			ctx    context.Context
			err    error
		)
		BeforeEach(func() {
			ctx = context.Background()

			bundle = &operatorv1alpha1.Operator{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "operator",
					Namespace:    "operator-controller-system",
				},
				Spec: operatorv1alpha1.OperatorSpec{
					PackageName: "my-cool-package",
				},
			}
			err = c.Create(ctx, bundle)
			Expect(err).To(Not(HaveOccurred()))
		})
		AfterEach(func() {
			By("deleting the testing Operator resource")
			err = c.Delete(ctx, bundle)
			Expect(err).To(Not(HaveOccurred()))
		})
		It("is reconciled", func() {
			// Note that we don't actually test anything since the reconciler currently does nothing
		})
	})
})
