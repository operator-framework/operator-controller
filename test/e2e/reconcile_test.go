package e2e

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

var _ = Describe("Operator Install", func() {
	When("a valid Operator CR specifying a package", func() {
		var (
			operator *operatorv1alpha1.Operator
			ctx      context.Context
			err      error
		)
		BeforeEach(func() {
			ctx = context.Background()

			operator = &operatorv1alpha1.Operator{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("operator-%s", rand.String(8)),
				},
				Spec: operatorv1alpha1.OperatorSpec{
					PackageName: "my-cool-package",
				},
			}
			err = c.Create(ctx, operator)
			Expect(err).To(Not(HaveOccurred()))
		})
		AfterEach(func() {
			By("deleting the testing Operator resource")
			err = c.Delete(ctx, operator)
			Expect(err).To(Not(HaveOccurred()))
		})
		PIt("installs the specified package", func() {
			// Pending until we actually have some code to test
			// Expect that a CRD and Deployment were successfully installed by rukpak
		})
	})
})
