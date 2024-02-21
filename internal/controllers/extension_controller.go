/*
Copyright 2024.

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

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/controllers/validators"
	"github.com/operator-framework/operator-controller/pkg/features"
)

// ExtensionReconciler reconciles a Extension object
type ExtensionReconciler struct {
	client.Client
}

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=extensions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=extensions/status,verbs=update;patch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=extensions/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("extension-controller")
	l.V(1).Info("starting")
	defer l.V(1).Info("ending")

	var existingExt = &ocv1alpha1.Extension{}
	if err := r.Client.Get(ctx, req.NamespacedName, existingExt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	reconciledExt := existingExt.DeepCopy()
	res, reconcileErr := r.reconcile(ctx, reconciledExt)

	// Do checks before any Update()s, as Update() may modify the resource structure!
	updateStatus := !equality.Semantic.DeepEqual(existingExt.Status, reconciledExt.Status)
	updateFinalizers := !equality.Semantic.DeepEqual(existingExt.Finalizers, reconciledExt.Finalizers)
	unexpectedFieldsChanged := r.checkForUnexpectedFieldChange(*existingExt, *reconciledExt)

	if updateStatus {
		if updateErr := r.Client.Status().Update(ctx, reconciledExt); updateErr != nil {
			return res, utilerrors.NewAggregate([]error{reconcileErr, updateErr})
		}
	}

	if unexpectedFieldsChanged {
		panic("spec or metadata changed by reconciler")
	}

	if updateFinalizers {
		if updateErr := r.Client.Update(ctx, reconciledExt); updateErr != nil {
			return res, utilerrors.NewAggregate([]error{reconcileErr, updateErr})
		}
	}

	return res, reconcileErr
}

// Compare resources - ignoring status & metadata.finalizers
func (*ExtensionReconciler) checkForUnexpectedFieldChange(a, b ocv1alpha1.Extension) bool {
	a.Status, b.Status = ocv1alpha1.ExtensionStatus{}, ocv1alpha1.ExtensionStatus{}
	a.Finalizers, b.Finalizers = []string{}, []string{}
	return !equality.Semantic.DeepEqual(a, b)
}

// Helper function to do the actual reconcile
//
// Today we always return ctrl.Result{} and an error.
// But in the future we might update this function
// to return different results (e.g. requeue).
//
//nolint:unparam
func (r *ExtensionReconciler) reconcile(ctx context.Context, ext *ocv1alpha1.Extension) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("extension-controller")

	// Don't do anything if feature gated
	if !features.OperatorControllerFeatureGate.Enabled(features.EnableExtensionAPI) {
		l.Info("extension feature is gated", "name", ext.GetName(), "namespace", ext.GetNamespace())

		// Set the TypeInstalled condition to Unknown to indicate that the resolution
		// hasn't been attempted yet, due to the spec being invalid.
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, "extension feature is disabled", ext.GetGeneration())
		// Set the TypeResolved condition to Unknown to indicate that the resolution
		// hasn't been attempted yet, due to the spec being invalid.
		ext.Status.ResolvedBundleResource = ""
		setResolvedStatusConditionUnknown(&ext.Status.Conditions, "extension feature is disabled", ext.GetGeneration())

		setDeprecationStatusesUnknown(&ext.Status.Conditions, "extension feature is disabled", ext.GetGeneration())
		return ctrl.Result{}, nil
	}

	// Don't do anything if Paused
	if ext.Spec.Managed == ocv1alpha1.ManagedStatePaused {
		l.Info("resource is paused", "name", ext.GetName(), "namespace", ext.GetNamespace())
		return ctrl.Result{}, nil
	}

	// validate spec
	if err := validators.ValidateExtensionSpec(ext); err != nil {
		// Set the TypeInstalled condition to Unknown to indicate that the resolution
		// hasn't been attempted yet, due to the spec being invalid.
		ext.Status.InstalledBundleResource = ""
		setInstalledStatusConditionUnknown(&ext.Status.Conditions, "installation has not been attempted as spec is invalid", ext.GetGeneration())
		// Set the TypeResolved condition to Unknown to indicate that the resolution
		// hasn't been attempted yet, due to the spec being invalid.
		ext.Status.ResolvedBundleResource = ""
		setResolvedStatusConditionUnknown(&ext.Status.Conditions, "validation has not been attempted as spec is invalid", ext.GetGeneration())

		setDeprecationStatusesUnknown(&ext.Status.Conditions, "deprecation checks have not been attempted as spec is invalid", ext.GetGeneration())
		return ctrl.Result{}, nil
	}

	// TODO: kapp-controller integration
	// * gather variables for resolution
	// * perform resolution
	// * lookup the bundle in the solution/selection that corresponds to the Extension's desired Source
	// * set the status of the Extension based on the respective deployed application status conditions.

	// Set the TypeInstalled condition to Unknown to indicate that the resolution
	// hasn't been attempted yet, due to the spec being invalid.
	ext.Status.InstalledBundleResource = ""
	setInstalledStatusConditionUnknown(&ext.Status.Conditions, "the Extension interface is not fully implemented", ext.GetGeneration())
	// Set the TypeResolved condition to Unknown to indicate that the resolution
	// hasn't been attempted yet, due to the spec being invalid.
	ext.Status.ResolvedBundleResource = ""
	setResolvedStatusConditionUnknown(&ext.Status.Conditions, "the Extension interface is not fully implemented", ext.GetGeneration())

	setDeprecationStatusesUnknown(&ext.Status.Conditions, "the Extension interface is not fully implemented", ext.GetGeneration())

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ExtensionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// TODO: Add watch for kapp-controller resources

	// When feature-gated, don't watch catalogs.
	if !features.OperatorControllerFeatureGate.Enabled(features.EnableExtensionAPI) {
		return ctrl.NewControllerManagedBy(mgr).
			For(&ocv1alpha1.Extension{}).
			Complete(r)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&ocv1alpha1.Extension{}).
		Watches(&catalogd.Catalog{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
