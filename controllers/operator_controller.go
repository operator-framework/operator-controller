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

	"github.com/operator-framework/operator-controller/controllers/validators"

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

//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=bundlemetadata,verbs=list;watch
//+kubebuilder:rbac:groups=catalogd.operatorframework.io,resources=packages,verbs=list;watch

func (r *OperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("operator-controller")
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
	// validate spec
	if err := validators.ValidateOperatorSpec(op); err != nil {
		// Set the TypeInstalled condition to Unknown to indicate that the resolution
		// hasn't been attempted yet, due to the spec being invalid.
		op.Status.InstalledBundleSource = ""
		apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
			Type:               operatorsv1alpha1.TypeInstalled,
			Status:             metav1.ConditionUnknown,
			Reason:             operatorsv1alpha1.ReasonInstallationStatusUnknown,
			Message:            "installation has not been attempted as spec is invalid",
			ObservedGeneration: op.GetGeneration(),
		})
		// Set the TypeResolved condition to Unknown to indicate that the resolution
		// hasn't been attempted yet, due to the spec being invalid.
		op.Status.ResolvedBundleResource = ""
		apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
			Type:               operatorsv1alpha1.TypeResolved,
			Status:             metav1.ConditionUnknown,
			Reason:             operatorsv1alpha1.ReasonResolutionUnknown,
			Message:            "validation has not been attempted as spec is invalid",
			ObservedGeneration: op.GetGeneration(),
		})
		return ctrl.Result{}, nil
	}
	// run resolution
	solution, err := r.Resolver.Resolve(ctx)
	if err != nil {
		op.Status.InstalledBundleSource = ""
		apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
			Type:               operatorsv1alpha1.TypeInstalled,
			Status:             metav1.ConditionUnknown,
			Reason:             operatorsv1alpha1.ReasonInstallationStatusUnknown,
			Message:            "installation has not been attempted as resolution failed",
			ObservedGeneration: op.GetGeneration(),
		})
		op.Status.ResolvedBundleResource = ""
		apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
			Type:               operatorsv1alpha1.TypeResolved,
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
		op.Status.InstalledBundleSource = ""
		apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
			Type:               operatorsv1alpha1.TypeInstalled,
			Status:             metav1.ConditionUnknown,
			Reason:             operatorsv1alpha1.ReasonInstallationStatusUnknown,
			Message:            "installation has not been attempted as resolution failed",
			ObservedGeneration: op.GetGeneration(),
		})
		op.Status.ResolvedBundleResource = ""
		apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
			Type:               operatorsv1alpha1.TypeResolved,
			Status:             metav1.ConditionFalse,
			Reason:             operatorsv1alpha1.ReasonResolutionFailed,
			Message:            err.Error(),
			ObservedGeneration: op.GetGeneration(),
		})
		return ctrl.Result{}, err
	}

	// Get the bundle image reference for the bundle
	bundleImage, err := bundleEntity.BundlePath()
	if err != nil {
		op.Status.InstalledBundleSource = ""
		apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
			Type:               operatorsv1alpha1.TypeInstalled,
			Status:             metav1.ConditionUnknown,
			Reason:             operatorsv1alpha1.ReasonInstallationStatusUnknown,
			Message:            "installation has not been attempted as resolution failed",
			ObservedGeneration: op.GetGeneration(),
		})
		op.Status.ResolvedBundleResource = ""
		apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
			Type:               operatorsv1alpha1.TypeResolved,
			Status:             metav1.ConditionFalse,
			Reason:             operatorsv1alpha1.ReasonResolutionFailed,
			Message:            err.Error(),
			ObservedGeneration: op.GetGeneration(),
		})
		return ctrl.Result{}, err
	}

	// Now we can set the Resolved Condition, and the resolvedBundleSource field to the bundleImage value.
	op.Status.ResolvedBundleResource = bundleImage
	apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
		Type:               operatorsv1alpha1.TypeResolved,
		Status:             metav1.ConditionTrue,
		Reason:             operatorsv1alpha1.ReasonSuccess,
		Message:            fmt.Sprintf("resolved to %q", bundleImage),
		ObservedGeneration: op.GetGeneration(),
	})

	// Ensure a BundleDeployment exists with its bundle source from the bundle
	// image we just looked up in the solution.
	dep := r.generateExpectedBundleDeployment(*op, bundleImage)
	if err := r.ensureBundleDeployment(ctx, dep); err != nil {
		// originally Reason: operatorsv1alpha1.ReasonInstallationFailed
		op.Status.InstalledBundleSource = ""
		apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
			Type:               operatorsv1alpha1.TypeInstalled,
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
		// originally Reason: operatorsv1alpha1.ReasonInstallationStatusUnknown
		op.Status.InstalledBundleSource = ""
		apimeta.SetStatusCondition(&op.Status.Conditions, metav1.Condition{
			Type:               operatorsv1alpha1.TypeInstalled,
			Status:             metav1.ConditionUnknown,
			Reason:             operatorsv1alpha1.ReasonInstallationStatusUnknown,
			Message:            err.Error(),
			ObservedGeneration: op.GetGeneration(),
		})
		return ctrl.Result{}, err
	}

	// Let's set the proper Installed condition and installedBundleSource field based on the
	// existing BundleDeployment object status.
	installedCond, installedBundleSource := mapBDStatusToInstalledCondition(existingTypedBundleDeployment, op)
	apimeta.SetStatusCondition(&op.Status.Conditions, installedCond)
	op.Status.InstalledBundleSource = installedBundleSource

	// set the status of the operator based on the respective bundle deployment status conditions.
	return ctrl.Result{}, nil
}

func mapBDStatusToInstalledCondition(existingTypedBundleDeployment *rukpakv1alpha1.BundleDeployment, op *operatorsv1alpha1.Operator) (metav1.Condition, string) {
	bundleDeploymentReady := apimeta.FindStatusCondition(existingTypedBundleDeployment.Status.Conditions, rukpakv1alpha1.TypeInstalled)
	if bundleDeploymentReady == nil {
		return metav1.Condition{
			Type:               operatorsv1alpha1.TypeInstalled,
			Status:             metav1.ConditionUnknown,
			Reason:             operatorsv1alpha1.ReasonInstallationStatusUnknown,
			Message:            "bundledeployment status is unknown",
			ObservedGeneration: op.GetGeneration(),
		}, ""
	}

	if bundleDeploymentReady.Status != metav1.ConditionTrue {
		return metav1.Condition{
			Type:               operatorsv1alpha1.TypeInstalled,
			Status:             metav1.ConditionFalse,
			Reason:             operatorsv1alpha1.ReasonInstallationFailed,
			Message:            fmt.Sprintf("bundledeployment not ready: %s", bundleDeploymentReady.Message),
			ObservedGeneration: op.GetGeneration(),
		}, ""
	}

	bundleDeploymentSource := existingTypedBundleDeployment.Spec.Template.Spec.Source
	switch bundleDeploymentSource.Type {
	case rukpakv1alpha1.SourceTypeImage:
		return metav1.Condition{
			Type:               operatorsv1alpha1.TypeInstalled,
			Status:             metav1.ConditionTrue,
			Reason:             operatorsv1alpha1.ReasonSuccess,
			Message:            fmt.Sprintf("installed from %q", bundleDeploymentSource.Image.Ref),
			ObservedGeneration: op.GetGeneration(),
		}, bundleDeploymentSource.Image.Ref
	case rukpakv1alpha1.SourceTypeGit:
		return metav1.Condition{
			Type:               operatorsv1alpha1.TypeInstalled,
			Status:             metav1.ConditionTrue,
			Reason:             operatorsv1alpha1.ReasonSuccess,
			Message:            fmt.Sprintf("installed from %q", bundleDeploymentSource.Git.Repository+"@"+bundleDeploymentSource.Git.Ref.Commit),
			ObservedGeneration: op.GetGeneration(),
		}, bundleDeploymentSource.Git.Repository + "@" + bundleDeploymentSource.Git.Ref.Commit
	default:
		return metav1.Condition{
			Type:               operatorsv1alpha1.TypeInstalled,
			Status:             metav1.ConditionUnknown,
			Reason:             operatorsv1alpha1.ReasonInstallationStatusUnknown,
			Message:            fmt.Sprintf("unknown bundledeployment source type %q", bundleDeploymentSource.Type),
			ObservedGeneration: op.GetGeneration(),
		}, ""
	}
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

	if isValidBundleCond != nil && isValidBundleCond.Status == metav1.ConditionFalse {
		return metav1.ConditionFalse, isValidBundleCond.Message
	}

	if isInstalledCond != nil && isInstalledCond.Status == metav1.ConditionFalse {
		return metav1.ConditionFalse, isInstalledCond.Message
	}

	if isInstalledCond != nil && isInstalledCond.Status == metav1.ConditionTrue {
		return metav1.ConditionTrue, "install was successful"
	}
	return metav1.ConditionUnknown, fmt.Sprintf("could not determine the state of BundleDeployment %s", dep.Name)
}

// isBundleDepStale returns true if conditions are out of date.
func isBundleDepStale(bd *rukpakv1alpha1.BundleDeployment) bool {
	return bd != nil && bd.Status.ObservedGeneration != bd.GetGeneration()
}
