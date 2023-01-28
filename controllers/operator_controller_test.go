package controllers_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/resolution"
	operatorutil "github.com/operator-framework/operator-controller/internal/util"
)

var _ = Describe("Reconcile Test", func() {
	var (
		ctx        context.Context
		reconciler *controllers.OperatorReconciler
	)
	BeforeEach(func() {
		ctx = context.Background()
		reconciler = &controllers.OperatorReconciler{
			Client:   cl,
			Scheme:   sch,
			Resolver: resolution.NewOperatorResolver(cl, testEntitySource),
		}
	})
	When("the operator does not exist", func() {
		It("returns no error", func() {
			res, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "non-existent"}})
			Expect(res).To(Equal(ctrl.Result{}))
			Expect(err).NotTo(HaveOccurred())
		})
	})
	When("the operator exists", func() {
		var (
			operator *operatorsv1alpha1.Operator
			opKey    types.NamespacedName
		)
		BeforeEach(func() {
			opKey = types.NamespacedName{Name: fmt.Sprintf("operator-test-%s", rand.String(8))}
		})
		When("the operator specifies a non-existent package", func() {
			var pkgName string
			BeforeEach(func() {
				By("initializing cluster state")
				pkgName = fmt.Sprintf("non-existent-%s", rand.String(6))
				operator = &operatorsv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
					Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
				}
				err := cl.Create(ctx, operator)
				Expect(err).NotTo(HaveOccurred())

				By("running reconcile")
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				Expect(res).To(Equal(ctrl.Result{}))
				Expect(err).NotTo(HaveOccurred())

				By("fetching updated operator after reconcile")
				Expect(cl.Get(ctx, opKey, operator)).NotTo(HaveOccurred())
			})
			It("sets resolution failure status", func() {
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeReady)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonResolutionFailed))
				Expect(cond.Message).To(Equal(fmt.Sprintf("package '%s' not found", pkgName)))
			})
		})
		When("the operator specifies a valid available package", func() {
			// TODO: add sub-scenarios -- When("BD does not exist") and When("BD already exists").
			const pkgName = "prometheus"
			BeforeEach(func() {
				By("initializing cluster state")
				operator = &operatorsv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
					Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
				}
				err := cl.Create(ctx, operator)
				Expect(err).NotTo(HaveOccurred())

				By("running reconcile")
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				Expect(res).To(Equal(ctrl.Result{}))
				Expect(err).NotTo(HaveOccurred())

				By("fetching updated operator after reconcile")
				Expect(cl.Get(ctx, opKey, operator)).NotTo(HaveOccurred())
			})
			It("results in the expected BundleDeployment", func() {
				bd := &rukpakv1alpha1.BundleDeployment{}
				err := cl.Get(ctx, types.NamespacedName{Name: opKey.Name}, bd)
				Expect(err).NotTo(HaveOccurred())
				// TODO: verify other fields
			})
			It("sets resolution success status", func() {
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeReady)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonResolutionSucceeded))
				Expect(cond.Message).To(Equal("resolution was successful"))
			})
		})
		When("the selected bundle's image ref cannot be parsed", func() {
			const pkgName = "badimage"
			BeforeEach(func() {
				By("initializing cluster state")
				operator = &operatorsv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
					Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
				}
				err := cl.Create(ctx, operator)
				Expect(err).NotTo(HaveOccurred())
			})
			It("sets resolution failure status and returns an error", func() {
				By("running reconcile")
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				Expect(res).To(Equal(ctrl.Result{}))
				Expect(err).To(MatchError(ContainSubstring(`error determining bundle path for entity`)))

				By("fetching updated operator after reconcile")
				Expect(cl.Get(ctx, opKey, operator)).NotTo(HaveOccurred())

				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeReady)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonBundleLookupFailed))
				Expect(cond.Message).To(ContainSubstring(`error determining bundle path for entity`))
			})
		})
		When("the operator specifies a duplicate package", func() {
			const pkgName = "prometheus"
			BeforeEach(func() {
				By("initializing cluster state")
				err := cl.Create(ctx, &operatorsv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("orig-%s", opKey.Name)},
					Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
				})
				Expect(err).NotTo(HaveOccurred())

				operator = &operatorsv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
					Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
				}
				err = cl.Create(ctx, operator)
				Expect(err).NotTo(HaveOccurred())

				By("running reconcile")
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				Expect(res).To(Equal(ctrl.Result{}))
				Expect(err).To(BeNil())

				By("fetching updated operator after reconcile")
				Expect(cl.Get(ctx, opKey, operator)).NotTo(HaveOccurred())
			})
			It("sets resolution failure status", func() {
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeReady)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonResolutionFailed))
				Expect(cond.Message).To(Equal(`duplicate identifier "required package prometheus" in input`))
			})
		})
		AfterEach(func() {
			verifyInvariants(ctx, operator)

			err := cl.Delete(ctx, operator)
			Expect(err).To(Not(HaveOccurred()))
		})
	})
})

func verifyInvariants(ctx context.Context, op *operatorsv1alpha1.Operator) {
	key := client.ObjectKeyFromObject(op)
	err := cl.Get(ctx, key, op)
	Expect(err).To(BeNil())

	verifyConditionsInvariants(op)
}

func verifyConditionsInvariants(op *operatorsv1alpha1.Operator) {
	// Expect that the operator's set of conditions contains all defined
	// condition types for the Operator API. Every reconcile should always
	// ensure every condition type's status/reason/message reflects the state
	// read during _this_ reconcile call.
	Expect(op.Status.Conditions).To(HaveLen(len(operatorutil.ConditionTypes)))
	for _, t := range operatorutil.ConditionTypes {
		cond := apimeta.FindStatusCondition(op.Status.Conditions, t)
		Expect(cond).To(Not(BeNil()))
		Expect(cond.Status).NotTo(BeEmpty())
		Expect(cond.Reason).To(BeElementOf(operatorutil.ConditionReasons))
		Expect(cond.ObservedGeneration).To(Equal(op.GetGeneration()))
	}
}

var testEntitySource = input.NewCacheQuerier(map[deppy.Identifier]input.Entity{
	"operatorhub/prometheus/0.37.0": *input.NewEntity("operatorhub/prometheus/0.37.0", map[string]string{
		"olm.bundle.path": `"quay.io/operatorhubio/prometheus@sha256:3e281e587de3d03011440685fc4fb782672beab044c1ebadc42788ce05a21c35"`,
		"olm.channel":     `{"channelName":"beta","priority":0}`,
		"olm.package":     `{"packageName":"prometheus","version":"0.37.0"}`,
	}),
	"operatorhub/prometheus/0.47.0": *input.NewEntity("operatorhub/prometheus/0.47.0", map[string]string{
		"olm.bundle.path": `"quay.io/operatorhubio/prometheus@sha256:5b04c49d8d3eff6a338b56ec90bdf491d501fe301c9cdfb740e5bff6769a21ed"`,
		"olm.channel":     `{"channelName":"beta","priority":0,"replaces":"prometheusoperator.0.37.0"}`,
		"olm.package":     `{"packageName":"prometheus","version":"0.47.0"}`,
	}),
	"operatorhub/badimage/0.1.0": *input.NewEntity("operatorhub/badimage/0.1.0", map[string]string{
		"olm.bundle.path": `{"name": "quay.io/operatorhubio/badimage:v0.1.0"}`,
		"olm.package":     `{"packageName":"badimage","version":"0.1.0"}`,
	}),
})
