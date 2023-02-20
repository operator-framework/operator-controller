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
	"k8s.io/utils/pointer"
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
			})
			It("sets resolution failure status", func() {
				By("running reconcile")
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				Expect(res).To(Equal(ctrl.Result{}))
				Expect(err).To(MatchError(fmt.Sprintf("package '%s' not found", pkgName)))

				By("fetching updated operator after reconcile")
				Expect(cl.Get(ctx, opKey, operator)).NotTo(HaveOccurred())

				By("checking the expected conditions")
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeReady)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonResolutionFailed))
				Expect(cond.Message).To(Equal(fmt.Sprintf("package '%s' not found", pkgName)))
			})
		})
		When("the operator specifies a valid available package", func() {
			const pkgName = "prometheus"
			BeforeEach(func() {
				By("initializing cluster state")
				operator = &operatorsv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
					Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
				}
				err := cl.Create(ctx, operator)
				Expect(err).NotTo(HaveOccurred())
			})

			When("the BundleDeployment does not exist", func() {
				BeforeEach(func() {
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
					Expect(bd.Spec.ProvisionerClassName).To(Equal("core-rukpak-io-plain"))
					Expect(bd.Spec.Template.Spec.ProvisionerClassName).To(Equal("core-rukpak-io-registry"))
					Expect(bd.Spec.Template.Spec.Source.Type).To(Equal(rukpakv1alpha1.SourceTypeImage))
					Expect(bd.Spec.Template.Spec.Source.Image).NotTo(BeNil())
					Expect(bd.Spec.Template.Spec.Source.Image.Ref).To(Equal("quay.io/operatorhubio/prometheus@sha256:5b04c49d8d3eff6a338b56ec90bdf491d501fe301c9cdfb740e5bff6769a21ed"))
				})
				It("sets the status on operator", func() {
					cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeReady)
					Expect(cond).NotTo(BeNil())
					Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
					Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonInstallationStatusUnknown))
					Expect(cond.Message).To(ContainSubstring("waiting for bundleDeployment"))
				})
			})
			When("the expected BundleDeployment already exists", func() {
				BeforeEach(func() {
					By("patching the existing BD")
					err := cl.Create(ctx, &rukpakv1alpha1.BundleDeployment{
						ObjectMeta: metav1.ObjectMeta{
							Name: opKey.Name,
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion:         operatorsv1alpha1.GroupVersion.String(),
									Kind:               "Operator",
									Name:               operator.Name,
									UID:                operator.UID,
									Controller:         pointer.Bool(true),
									BlockOwnerDeletion: pointer.Bool(true),
								},
							},
						},
						Spec: rukpakv1alpha1.BundleDeploymentSpec{
							ProvisionerClassName: "core-rukpak-io-plain",
							Template: &rukpakv1alpha1.BundleTemplate{
								Spec: rukpakv1alpha1.BundleSpec{
									ProvisionerClassName: "core-rukpak-io-registry",
									Source: rukpakv1alpha1.BundleSource{
										Type: rukpakv1alpha1.SourceTypeImage,
										Image: &rukpakv1alpha1.ImageSource{
											Ref: "quay.io/operatorhubio/prometheus@sha256:5b04c49d8d3eff6a338b56ec90bdf491d501fe301c9cdfb740e5bff6769a21ed",
										},
									},
								},
							},
						},
					})
					Expect(err).NotTo(HaveOccurred())

					By("running reconcile")
					res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
					Expect(res).To(Equal(ctrl.Result{}))
					Expect(err).NotTo(HaveOccurred())

					By("fetching updated operator after reconcile")
					Expect(cl.Get(ctx, opKey, operator)).NotTo(HaveOccurred())
				})
				PIt("does not patch the BundleDeployment", func() {
					// TODO: verify that no patch call is made.
				})
				It("results in the expected BundleDeployment", func() {
					bd := &rukpakv1alpha1.BundleDeployment{}
					err := cl.Get(ctx, types.NamespacedName{Name: opKey.Name}, bd)
					Expect(err).NotTo(HaveOccurred())
					Expect(bd.Spec.ProvisionerClassName).To(Equal("core-rukpak-io-plain"))
					Expect(bd.Spec.Template.Spec.ProvisionerClassName).To(Equal("core-rukpak-io-registry"))
					Expect(bd.Spec.Template.Spec.Source.Type).To(Equal(rukpakv1alpha1.SourceTypeImage))
					Expect(bd.Spec.Template.Spec.Source.Image).NotTo(BeNil())
					Expect(bd.Spec.Template.Spec.Source.Image.Ref).To(Equal("quay.io/operatorhubio/prometheus@sha256:5b04c49d8d3eff6a338b56ec90bdf491d501fe301c9cdfb740e5bff6769a21ed"))
				})
				It("sets the status on operator", func() {
					cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeReady)
					Expect(cond).NotTo(BeNil())
					Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
					Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonInstallationStatusUnknown))
					Expect(cond.Message).To(ContainSubstring("waiting for bundleDeployment"))
				})
			})
			When("an out-of-date BundleDeployment exists", func() {
				BeforeEach(func() {
					By("creating the expected BD")
					err := cl.Create(ctx, &rukpakv1alpha1.BundleDeployment{
						ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
						Spec: rukpakv1alpha1.BundleDeploymentSpec{
							ProvisionerClassName: "foo",
							Template: &rukpakv1alpha1.BundleTemplate{
								Spec: rukpakv1alpha1.BundleSpec{
									ProvisionerClassName: "bar",
									Source: rukpakv1alpha1.BundleSource{
										Type: rukpakv1alpha1.SourceTypeHTTP,
										HTTP: &rukpakv1alpha1.HTTPSource{
											URL: "http://localhost:8080/",
										},
									},
								},
							},
						},
					})
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
					Expect(bd.Spec.ProvisionerClassName).To(Equal("core-rukpak-io-plain"))
					Expect(bd.Spec.Template.Spec.ProvisionerClassName).To(Equal("core-rukpak-io-registry"))
					Expect(bd.Spec.Template.Spec.Source.Type).To(Equal(rukpakv1alpha1.SourceTypeImage))
					Expect(bd.Spec.Template.Spec.Source.Image).NotTo(BeNil())
					Expect(bd.Spec.Template.Spec.Source.Image.Ref).To(Equal("quay.io/operatorhubio/prometheus@sha256:5b04c49d8d3eff6a338b56ec90bdf491d501fe301c9cdfb740e5bff6769a21ed"))
				})
				It("sets resolution success status", func() {
					cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeReady)
					Expect(cond).NotTo(BeNil())
					Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
					Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonInstallationStatusUnknown))
					Expect(cond.Message).To(ContainSubstring("waiting for bundleDeployment"))
				})
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

				By("checking the expected conditions")
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeReady)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonBundleLookupFailed))
				Expect(cond.Message).To(ContainSubstring(`error determining bundle path for entity`))
			})
		})
		When("the operator specifies a duplicate package", func() {
			const pkgName = "prometheus"
			var dupOperator *operatorsv1alpha1.Operator

			BeforeEach(func() {
				By("initializing cluster state")
				dupOperator = &operatorsv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("orig-%s", opKey.Name)},
					Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
				}

				err := cl.Create(ctx, dupOperator)
				Expect(err).NotTo(HaveOccurred())

				operator = &operatorsv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
					Spec:       operatorsv1alpha1.OperatorSpec{PackageName: pkgName},
				}
				err = cl.Create(ctx, operator)
				Expect(err).NotTo(HaveOccurred())
			})

			AfterEach(func() {
				err := cl.Delete(ctx, dupOperator)
				Expect(err).NotTo(HaveOccurred())
			})

			It("sets resolution failure status", func() {
				By("running reconcile")
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				Expect(res).To(Equal(ctrl.Result{}))
				Expect(err).To(MatchError(Equal(`duplicate identifier "required package prometheus" in input`)))

				By("fetching updated operator after reconcile")
				Expect(cl.Get(ctx, opKey, operator)).NotTo(HaveOccurred())

				By("checking the expected conditions")
				cond := apimeta.FindStatusCondition(operator.Status.Conditions, operatorsv1alpha1.TypeReady)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonResolutionFailed))
				Expect(cond.Message).To(Equal(`duplicate identifier "required package prometheus" in input`))
			})
		})
		When("the existing operator status is based on bundleDeployment", func() {
			const pkgName = "prometheus"
			var (
				bd *rukpakv1alpha1.BundleDeployment
			)
			BeforeEach(func() {
				By("creating the expected BundleDeployment")
				bd = &rukpakv1alpha1.BundleDeployment{
					ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
					Spec: rukpakv1alpha1.BundleDeploymentSpec{
						ProvisionerClassName: "core-rukpak-io-plain",
						Template: &rukpakv1alpha1.BundleTemplate{
							Spec: rukpakv1alpha1.BundleSpec{
								ProvisionerClassName: "core-rukpak-io-registry",
								Source: rukpakv1alpha1.BundleSource{
									Type: rukpakv1alpha1.SourceTypeImage,
									Image: &rukpakv1alpha1.ImageSource{
										Ref: "quay.io/operatorhubio/prometheus@sha256:5b04c49d8d3eff6a338b56ec90bdf491d501fe301c9cdfb740e5bff6769a21ed",
									},
								},
							},
						},
					},
				}
				err := cl.Create(ctx, bd)
				Expect(err).NotTo(HaveOccurred())

				By("creating the operator object")
				operator = &operatorsv1alpha1.Operator{
					ObjectMeta: metav1.ObjectMeta{Name: opKey.Name},
					Spec: operatorsv1alpha1.OperatorSpec{
						PackageName: pkgName,
					},
				}
				err = cl.Create(ctx, operator)
				Expect(err).NotTo(HaveOccurred())

			})

			AfterEach(func() {
				err := cl.Delete(ctx, bd)
				Expect(err).NotTo(HaveOccurred())
			})

			It("verify operator status when bundle deployment is waiting to be created", func() {
				By("running reconcile")
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				Expect(res).To(Equal(ctrl.Result{}))
				Expect(err).NotTo(HaveOccurred())

				By("fetching the updated operator after reconcile")
				op := &operatorsv1alpha1.Operator{}
				err = cl.Get(ctx, opKey, op)
				Expect(err).NotTo(HaveOccurred())

				By("checking the expected conditions")
				cond := apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeReady)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
				Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonInstallationStatusUnknown))
				Expect(cond.Message).To(ContainSubstring(`waiting for bundleDeployment`))
			})

			It("verify operator status when `HasValidBundle` condition of rukpak is false", func() {
				apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
					Type:    rukpakv1alpha1.TypeHasValidBundle,
					Status:  metav1.ConditionFalse,
					Message: "failed to unpack",
					Reason:  rukpakv1alpha1.ReasonUnpackFailed,
				})

				By("updating the status of bundleDeployment")
				err := cl.Status().Update(ctx, bd)
				Expect(err).NotTo(HaveOccurred())

				By("running reconcile")
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				Expect(res).To(Equal(ctrl.Result{}))
				Expect(err).NotTo(HaveOccurred())

				By("fetching the updated operator after reconcile")
				op := &operatorsv1alpha1.Operator{}
				err = cl.Get(ctx, opKey, op)
				Expect(err).NotTo(HaveOccurred())

				By("checking the expected conditions")
				cond := apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeReady)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonInstallationFailed))
				Expect(cond.Message).To(ContainSubstring(`failed to unpack`))
			})

			It("verify operator status when `InstallReady` condition of rukpak is false", func() {
				apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
					Type:    rukpakv1alpha1.TypeInstalled,
					Status:  metav1.ConditionFalse,
					Message: "failed to install",
					Reason:  rukpakv1alpha1.ReasonInstallFailed,
				})

				By("updating the status of bundleDeployment")
				err := cl.Status().Update(ctx, bd)
				Expect(err).NotTo(HaveOccurred())

				By("running reconcile")
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				Expect(res).To(Equal(ctrl.Result{}))
				Expect(err).NotTo(HaveOccurred())

				By("fetching the updated operator after reconcile")
				op := &operatorsv1alpha1.Operator{}
				err = cl.Get(ctx, opKey, op)
				Expect(err).NotTo(HaveOccurred())

				By("checking the expected conditions")
				cond := apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeReady)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonInstallationFailed))
				Expect(cond.Message).To(ContainSubstring(`failed to install`))
			})

			It("verify operator status when `InstallReady` condition of rukpak is true", func() {
				apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
					Type:    rukpakv1alpha1.TypeInstalled,
					Status:  metav1.ConditionTrue,
					Message: "operator installed successfully",
					Reason:  rukpakv1alpha1.ReasonInstallationSucceeded,
				})

				By("updating the status of bundleDeployment")
				err := cl.Status().Update(ctx, bd)
				Expect(err).NotTo(HaveOccurred())

				By("running reconcile")
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				Expect(res).To(Equal(ctrl.Result{}))
				Expect(err).NotTo(HaveOccurred())

				By("fetching the updated operator after reconcile")
				op := &operatorsv1alpha1.Operator{}
				err = cl.Get(ctx, opKey, op)
				Expect(err).NotTo(HaveOccurred())

				By("checking the expected conditions")
				cond := apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeReady)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonInstallationSucceeded))
				Expect(cond.Message).To(ContainSubstring(`install was successful`))
			})

			It("verify any other unknown status of bundledeployment", func() {
				apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
					Type:    rukpakv1alpha1.TypeHasValidBundle,
					Status:  metav1.ConditionUnknown,
					Message: "unpacking",
					Reason:  rukpakv1alpha1.ReasonUnpackSuccessful,
				})

				apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
					Type:    rukpakv1alpha1.TypeInstalled,
					Status:  metav1.ConditionUnknown,
					Message: "installing",
					Reason:  rukpakv1alpha1.ReasonInstallationSucceeded,
				})

				By("updating the status of bundleDeployment")
				err := cl.Status().Update(ctx, bd)
				Expect(err).NotTo(HaveOccurred())

				By("running reconcile")
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				Expect(res).To(Equal(ctrl.Result{}))
				Expect(err).NotTo(HaveOccurred())

				By("fetching the updated operator after reconcile")
				op := &operatorsv1alpha1.Operator{}
				err = cl.Get(ctx, opKey, op)
				Expect(err).NotTo(HaveOccurred())

				By("checking the expected conditions")
				cond := apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeReady)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
				Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonInstallationStatusUnknown))
				Expect(cond.Message).To(ContainSubstring(`could not determine the state of bundleDeployment`))
			})

			It("verify operator status when bundleDeployment installation status is unknown", func() {
				apimeta.SetStatusCondition(&bd.Status.Conditions, metav1.Condition{
					Type:    rukpakv1alpha1.TypeInstalled,
					Status:  metav1.ConditionUnknown,
					Message: "installing",
					Reason:  rukpakv1alpha1.ReasonInstallationSucceeded,
				})

				By("updating the status of bundleDeployment")
				err := cl.Status().Update(ctx, bd)
				Expect(err).NotTo(HaveOccurred())

				By("running reconcile")
				res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: opKey})
				Expect(res).To(Equal(ctrl.Result{}))
				Expect(err).NotTo(HaveOccurred())

				By("fetching the updated operator after reconcile")
				op := &operatorsv1alpha1.Operator{}
				err = cl.Get(ctx, opKey, op)
				Expect(err).NotTo(HaveOccurred())

				By("checking the expected conditions")
				cond := apimeta.FindStatusCondition(op.Status.Conditions, operatorsv1alpha1.TypeReady)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
				Expect(cond.Reason).To(Equal(operatorsv1alpha1.ReasonInstallationStatusUnknown))
				Expect(cond.Message).To(ContainSubstring(`could not determine the state of bundleDeployment`))
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
		"olm.gvk":         `[]`,
	}),
	"operatorhub/prometheus/0.47.0": *input.NewEntity("operatorhub/prometheus/0.47.0", map[string]string{
		"olm.bundle.path": `"quay.io/operatorhubio/prometheus@sha256:5b04c49d8d3eff6a338b56ec90bdf491d501fe301c9cdfb740e5bff6769a21ed"`,
		"olm.channel":     `{"channelName":"beta","priority":0,"replaces":"prometheusoperator.0.37.0"}`,
		"olm.package":     `{"packageName":"prometheus","version":"0.47.0"}`,
		"olm.gvk":         `[]`,
	}),
	"operatorhub/badimage/0.1.0": *input.NewEntity("operatorhub/badimage/0.1.0", map[string]string{
		"olm.bundle.path": `{"name": "quay.io/operatorhubio/badimage:v0.1.0"}`,
		"olm.package":     `{"packageName":"badimage","version":"0.1.0"}`,
		"olm.gvk":         `[]`,
	}),
})
