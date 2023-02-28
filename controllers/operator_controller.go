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

	"github.com/operator-framework/deppy/pkg/deppy/solver"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity"
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
		return ctrl.Result{}, err
	}

	// lookup the bundle entity in the solution that corresponds to the
	// Operator's desired package name.
	bundleEntity, err := r.getBundleEntityFromSolution(solution, op.Spec.PackageName)
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

	// Get the bundle image reference for the bundle
	bundleImage, err := bundleEntity.BundlePath()
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

	// Ensure a BundleDeployment exists with its bundle source from the bundle
	// image we just looked up in the solution.
	dep := r.generateExpectedBundleDeployment(*op, bundleImage)
	if err := r.ensureBundleDeployment(ctx, dep); err != nil {
		apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
			Type:               operatorsv1alpha1.TypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             operatorsv1alpha1.ReasonInstallationFailed,
			Message:            err.Error(),
			ObservedGeneration: op.GetGeneration(),
		})
		return ctrl.Result{}, err
	}

	// convert existing unstructured object into bundleDeployment for easier mapping of status.
	existingTypedBundleDeployment := &rukpakv1alpha1.BundleDeployment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(dep.UnstructuredContent(), existingTypedBundleDeployment); err != nil {
		apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
			Type:               operatorsv1alpha1.TypeReady,
			Status:             metav1.ConditionUnknown,
			Reason:             operatorsv1alpha1.ReasonInstallationStatusUnknown,
			Message:            err.Error(),
			ObservedGeneration: op.GetGeneration(),
		})
		return ctrl.Result{}, err
	}

	// set the status of the operator based on the respective bundle deployment status conditions.
	apimeta.SetStatusCondition(&op.Status.Conditions, mapBDStatusToReadyCondition(existingTypedBundleDeployment, op.GetGeneration()))
	return ctrl.Result{}, nil
}

func (r *OperatorReconciler) getBundleEntityFromSolution(solution *solver.Solution, packageName string) (*entity.BundleEntity, error) {
	for _, variable := range solution.SelectedVariables() {
		switch v := variable.(type) {
		case *bundles_and_dependencies.BundleVariable:
			entityPkgName, err := v.BundleEntity().PackageName()
			if err != nil {
				return nil, err
			}
			if packageName == entityPkgName {
				return v.BundleEntity(), nil
			}
		}
	}
	return nil, fmt.Errorf("entity for package %q not found in solution", packageName)
}

func (r *OperatorReconciler) generateExpectedBundleDeployment(o operatorsv1alpha1.Operator, bundlePath string) *unstructured.Unstructured {
	// We use unstructured here to avoid problems of serializing default values when sending patches to the apiserver.
	// If you use a typed object, any default values from that struct get serialized into the JSON patch, which could
	// cause unrelated fields to be patched back to the default value even though that isn't the intention. Using an
	// unstructured ensures that the patch contains only what is specified. Using unstructured like this is basically
	// identical to "kubectl apply -f"
	bd := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": rukpakv1alpha1.GroupVersion.String(),
		"kind":       rukpakv1alpha1.BundleDeploymentKind,
		"metadata": map[string]interface{}{
			"name": o.GetName(),
		},
		"spec": map[string]interface{}{
			// TODO: Don't assume plain provisioner
			"provisionerClassName": "core-rukpak-io-plain",
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					// TODO: Don't assume registry provisioner
					"provisionerClassName": "core-rukpak-io-registry",
					"source": map[string]interface{}{
						// TODO: Don't assume image type
						"type": string(rukpakv1alpha1.SourceTypeImage),
						"image": map[string]interface{}{
							"ref": bundlePath,
						},
					},
				},
			},
		},
	}}
	bd.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion:         operatorsv1alpha1.GroupVersion.String(),
			Kind:               "Operator",
			Name:               o.Name,
			UID:                o.UID,
			Controller:         pointer.Bool(true),
			BlockOwnerDeletion: pointer.Bool(true),
		},
	})
	return bd
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

func (r *OperatorReconciler) ensureBundleDeployment(ctx context.Context, desiredBundleDeployment *unstructured.Unstructured) error {
	// TODO: what if there happens to be an unrelated BD with the same name as the Operator?
	//   we should probably also check to see if there's an owner reference and/or a label set
	//   that we expect only to ever be used by the operator controller. That way, we don't
	//   automatically and silently adopt and change a BD that the user doens't intend to be
	//   owned by the Operator.
	existingBundleDeployment, err := r.existingBundleDeploymentUnstructured(ctx, desiredBundleDeployment.GetName())
	if client.IgnoreNotFound(err) != nil {
		return err
	}

	// If the existing BD already has everything that the desired BD has, no need to contact the API server.
	// Make sure the status of the existingBD from the server is as expected.
	if equality.Semantic.DeepDerivative(desiredBundleDeployment, existingBundleDeployment) {
		*desiredBundleDeployment = *existingBundleDeployment
		return nil
	}

	return r.Client.Patch(ctx, desiredBundleDeployment, client.Apply, client.ForceOwnership, client.FieldOwner("operator-controller"))
}

func (r *OperatorReconciler) existingBundleDeploymentUnstructured(ctx context.Context, name string) (*unstructured.Unstructured, error) {
	existingBundleDeployment := &rukpakv1alpha1.BundleDeployment{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: name}, existingBundleDeployment)
	if err != nil {
		return nil, err
	}
	existingBundleDeployment.APIVersion = rukpakv1alpha1.GroupVersion.String()
	existingBundleDeployment.Kind = rukpakv1alpha1.BundleDeploymentKind
	unstrExistingBundleDeploymentObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(existingBundleDeployment)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: unstrExistingBundleDeploymentObj}, nil
}

// verifyBDStatus reads the various possibilities of status in bundle deployment and translates
// into corresponding operator condition status and message.
func verifyBDStatus(dep *rukpakv1alpha1.BundleDeployment) (metav1.ConditionStatus, string) {
	isValidBundleCond := apimeta.FindStatusCondition(dep.Status.Conditions, rukpakv1alpha1.TypeHasValidBundle)
	isInstalledCond := apimeta.FindStatusCondition(dep.Status.Conditions, rukpakv1alpha1.TypeInstalled)

	if isValidBundleCond == nil && isInstalledCond == nil {
		return metav1.ConditionUnknown, fmt.Sprintf("waiting for bundleDeployment %q status to be updated", dep.Name)
	}

	if isValidBundleCond != nil && isValidBundleCond.Status == metav1.ConditionFalse {
		return metav1.ConditionFalse, isValidBundleCond.Message
	}

	if isInstalledCond != nil && isInstalledCond.Status == metav1.ConditionFalse {
		return metav1.ConditionFalse, isInstalledCond.Message
	}

	if isInstalledCond != nil && isInstalledCond.Status == metav1.ConditionTrue {
		return metav1.ConditionTrue, "install was successful"
	}
	return metav1.ConditionUnknown, fmt.Sprintf("could not determine the state of bundleDeployment %s", dep.Name)
}

// mapBDStatusToReadyCondition returns the operator object's "TypeReady" condition based on the bundle deployment statuses.
func mapBDStatusToReadyCondition(existingBD *rukpakv1alpha1.BundleDeployment, observedGeneration int64) metav1.Condition {
	// update operator status:
	// 1. If the Operator "Ready" status is "Unknown": The status of successful bundleDeployment is unknown, wait till Rukpak updates the BD status.
	// 2. If the Operator "Ready" status is "True": Update the "successful resolution" status and return the result.
	// 3. If the Operator "Ready" status is "False": There is error observed from Rukpak. Update the status accordingly.
	status, message := verifyBDStatus(existingBD)
	var reason string

	switch status {
	case metav1.ConditionTrue:
		reason = operatorsv1alpha1.ReasonInstallationSucceeded
	case metav1.ConditionFalse:
		reason = operatorsv1alpha1.ReasonInstallationFailed
	default:
		reason = operatorsv1alpha1.ReasonInstallationStatusUnknown
	}

	return metav1.Condition{
		Type:               operatorsv1alpha1.TypeReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: observedGeneration,
	}
}
