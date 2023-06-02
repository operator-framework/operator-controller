package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	operatorv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
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
				}
			})
			It("resolves the specified package with correct bundle path", func() {
				By("creating the Operator resource")
				Expect(c.Create(ctx, operator)).To(Succeed())

				By("eventually reporting a successful resolution and bundle path")
				Eventually(func(g Gomega) {
					g.Expect(c.Get(ctx, types.NamespacedName{Name: operator.Name}, operator)).To(Succeed())
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

					cond = apimeta.FindStatusCondition(bd.Status.Conditions, rukpakv1alpha1.TypeHasValidBundle)
					g.Expect(cond).ToNot(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(cond.Reason).To(Equal(rukpakv1alpha1.ReasonUnpackSuccessful))

					cond = apimeta.FindStatusCondition(bd.Status.Conditions, rukpakv1alpha1.TypeInstalled)
					g.Expect(cond).ToNot(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(cond.Reason).To(Equal(rukpakv1alpha1.ReasonInstallationSucceeded))
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

					cond = apimeta.FindStatusCondition(bd.Status.Conditions, rukpakv1alpha1.TypeHasValidBundle)
					g.Expect(cond).ToNot(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(cond.Reason).To(Equal(rukpakv1alpha1.ReasonUnpackSuccessful))

					cond = apimeta.FindStatusCondition(bd.Status.Conditions, rukpakv1alpha1.TypeInstalled)
					g.Expect(cond).ToNot(BeNil())
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(cond.Reason).To(Equal(rukpakv1alpha1.ReasonInstallationSucceeded))

				}).Should(Succeed())
			})
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
