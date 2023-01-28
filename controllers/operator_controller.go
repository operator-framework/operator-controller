/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/resolution"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/bundles_and_dependencies"
)

// OperatorReconciler reconciles a Operator object
type OperatorReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Resolver *resolution.OperatorResolver
}

//+kubebuilder:rbac:groups=operators.operatorframework.io,resources=operators,verbs=get;list;watch
//+kubebuilder:rbac:groups=operators.operatorframework.io,resources=operators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operators.operatorframework.io,resources=operators/finalizers,verbs=update
//+kubebuilder:rbac:groups=core.rukpak.io,resources=bundledeployments,verbs=get;list;watch;create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Operator object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.1/pkg/reconcile
func (r *OperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("reconcile")
	l.V(1).Info("starting")
	defer l.V(1).Info("ending")

	var existingOp = &operatorsv1alpha1.Operator{}
	if err := r.Get(ctx, req.NamespacedName, existingOp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	reconciledOp := existingOp.DeepCopy()
	res, reconcileErr := r.reconcile(ctx, reconciledOp)

	// Do checks before any Update()s, as Update() may modify the resource structure!
	updateStatus := !equality.Semantic.DeepEqual(existingOp.Status, reconciledOp.Status)
	updateFinalizers := !equality.Semantic.DeepEqual(existingOp.Finalizers, reconciledOp.Finalizers)
	unexpectedFieldsChanged := checkForUnexpectedFieldChange(*existingOp, *reconciledOp)

	if updateStatus {
		if updateErr := r.Status().Update(ctx, reconciledOp); updateErr != nil {
			return res, utilerrors.NewAggregate([]error{reconcileErr, updateErr})
		}
	}

	if unexpectedFieldsChanged {
		panic("spec or metadata changed by reconciler")
	}

	if updateFinalizers {
		if updateErr := r.Update(ctx, reconciledOp); updateErr != nil {
			return res, utilerrors.NewAggregate([]error{reconcileErr, updateErr})
		}
	}

	return res, reconcileErr
}

// Compare resources - ignoring status & metadata.finalizers
func checkForUnexpectedFieldChange(a, b operatorsv1alpha1.Operator) bool {
	a.Status, b.Status = operatorsv1alpha1.OperatorStatus{}, operatorsv1alpha1.OperatorStatus{}
	a.Finalizers, b.Finalizers = []string{}, []string{}
	return !equality.Semantic.DeepEqual(a, b)
}

// Helper function to do the actual reconcile
func (r *OperatorReconciler) reconcile(ctx context.Context, op *operatorsv1alpha1.Operator) (ctrl.Result, error) {

	// run resolution
	solution, err := r.Resolver.Resolve(ctx)
	if err != nil {
		apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
			Type:               operatorsv1alpha1.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             operatorsv1alpha1.ReasonResolutionFailed,
			Message:            err.Error(),
			ObservedGeneration: op.GetGeneration(),
		})
		return ctrl.Result{}, nil
	}

	// extract package bundle path from resolved variable
	packageFound := false
	for _, variable := range solution.SelectedVariables() {
		switch v := variable.(type) {
		case *bundles_and_dependencies.BundleVariable:
			packageName, err := v.BundleEntity().PackageName()
			if err != nil {
				apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
					Type:               operatorsv1alpha1.TypeReady,
					Status:             metav1.ConditionFalse,
					Reason:             operatorsv1alpha1.ReasonBundleLookupFailed,
					Message:            err.Error(),
					ObservedGeneration: op.GetGeneration(),
				})
				return ctrl.Result{}, err
			}
			if packageName == op.Spec.PackageName {
				bundlePath, err := v.BundleEntity().BundlePath()
				if err != nil {
					apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
						Type:               operatorsv1alpha1.TypeReady,
						Status:             metav1.ConditionFalse,
						Reason:             operatorsv1alpha1.ReasonBundleLookupFailed,
						Message:            err.Error(),
						ObservedGeneration: op.GetGeneration(),
					})
					return ctrl.Result{}, err
				}
				dep := r.generateExpectedBundleDeployment(*op, bundlePath)
				// Create bundleDeployment if not found or Update if needed
				if err := r.ensureBundleDeployment(ctx, dep); err != nil {
					apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
						Type:               operatorsv1alpha1.TypeReady,
						Status:             metav1.ConditionFalse,
						Reason:             operatorsv1alpha1.ReasonBundleDeploymentFailed,
						Message:            err.Error(),
						ObservedGeneration: op.GetGeneration(),
					})
					return ctrl.Result{}, err
				}
				packageFound = true
				break
			}
		}
	}
	if !packageFound {
		// TODO: If this happens, it very likely indicates a bug in our resolver.
		//    For that reason, should this just panic?
		apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
			Type:               operatorsv1alpha1.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             operatorsv1alpha1.ReasonBundleLookupFailed,
			Message:            fmt.Sprintf("resolved set does not contain expected package %q", op.Spec.PackageName),
			ObservedGeneration: op.GetGeneration(),
		})
		return ctrl.Result{}, nil
	}

	// update operator status
	apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
		Type:               operatorsv1alpha1.TypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             operatorsv1alpha1.ReasonResolutionSucceeded,
		Message:            "resolution was successful",
		ObservedGeneration: op.GetGeneration(),
	})

	return ctrl.Result{}, nil
}

func (r *OperatorReconciler) generateExpectedBundleDeployment(o operatorsv1alpha1.Operator, bundlePath string) *rukpakv1alpha1.BundleDeployment {
	// TODO: Use unstructured + server side apply?
	return &rukpakv1alpha1.BundleDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: o.GetName(),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         operatorsv1alpha1.GroupVersion.String(),
					Kind:               "Operator",
					Name:               o.Name,
					UID:                o.UID,
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
				},
			},
		},
		Spec: rukpakv1alpha1.BundleDeploymentSpec{
			//TODO: Don't assume plain provisioner
			ProvisionerClassName: "core-rukpak-io-plain",
			Template: &rukpakv1alpha1.BundleTemplate{
				ObjectMeta: metav1.ObjectMeta{
					// TODO: Remove
					Labels: map[string]string{
						"app": "my-bundle",
					},
				},
				Spec: rukpakv1alpha1.BundleSpec{
					Source: rukpakv1alpha1.BundleSource{
						// TODO: Don't assume image type
						Type: rukpakv1alpha1.SourceTypeImage,
						Image: &rukpakv1alpha1.ImageSource{
							Ref: bundlePath,
						},
					},

					//TODO: Don't assume registry provisioner
					ProvisionerClassName: "core-rukpak-io-registry",
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *OperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewControllerManagedBy(mgr).
		For(&operatorsv1alpha1.Operator{}).
		Owns(&rukpakv1alpha1.BundleDeployment{}).
		Complete(r)

	if err != nil {
		return err
	}
	return nil
}

func (r *OperatorReconciler) ensureBundleDeployment(ctx context.Context, desiredBundleDeployment *rukpakv1alpha1.BundleDeployment) error {
	existingBundleDeployment := &rukpakv1alpha1.BundleDeployment{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: desiredBundleDeployment.GetName()}, existingBundleDeployment)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		return r.Client.Create(ctx, desiredBundleDeployment)
	}

	// Check if the existing bundleDeployment's spec needs to be updated
	//
	// FIXME: checking only for spec differences means that we will not fixup
	//   changes to and removals of desired metadata.
	if equality.Semantic.DeepEqual(existingBundleDeployment.Spec, desiredBundleDeployment.Spec) {
		return nil
	}

	existingBundleDeployment.Spec = desiredBundleDeployment.Spec
	return r.Client.Update(ctx, existingBundleDeployment)
}
