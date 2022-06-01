/*
Copyright 2022.

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

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	platformv1alpha1 "github.com/timflannagan/platform-operators/api/v1alpha1"
	"github.com/timflannagan/platform-operators/internal/applier"
	"github.com/timflannagan/platform-operators/internal/sourcer"
	"github.com/timflannagan/platform-operators/internal/util"
)

// PlatformOperatorReconciler reconciles a PlatformOperator object
type PlatformOperatorReconciler struct {
	client.Client
	Sourcer sourcer.Sourcer
	Applier applier.Applier
	Scheme  *runtime.Scheme
}

//+kubebuilder:rbac:groups=platform.openshift.io,resources=platformoperators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=platform.openshift.io,resources=platformoperators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=platform.openshift.io,resources=platformoperators/finalizers,verbs=update
//+kubebuilder:rbac:groups=operators.coreos.com,resources=catalogsources,verbs=get;list;watch
//+kubebuilder:rbac:groups=core.rukpak.io,resources=bundleinstances,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.rukpak.io,resources=bundles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=*,resources=*,verbs=*

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *PlatformOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logr.FromContext(ctx)
	log.Info("reconciling request", "req", req.NamespacedName)
	defer log.Info("finished reconciling request", "req", req.NamespacedName)

	// TODO: flesh out status condition management
	po := &platformv1alpha1.PlatformOperator{}
	if err := r.Get(ctx, req.NamespacedName, po); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	defer func() {
		po := po.DeepCopy()
		po.ObjectMeta.ManagedFields = nil
		if err := r.Status().Patch(ctx, po, client.Apply, client.FieldOwner("platformoperator")); err != nil {
			log.Error(err, "failed to patch status")
		}
	}()

	desiredBundle, err := r.Sourcer.Source(ctx, po)
	if err != nil {
		meta.SetStatusCondition(&po.Status.Conditions, metav1.Condition{
			Type:    platformv1alpha1.TypeSourced,
			Status:  metav1.ConditionUnknown,
			Reason:  platformv1alpha1.ReasonSourceFailed,
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}
	meta.SetStatusCondition(&po.Status.Conditions, metav1.Condition{
		Type:    platformv1alpha1.TypeSourced,
		Status:  metav1.ConditionTrue,
		Reason:  platformv1alpha1.ReasonSourceSuccessful,
		Message: "Successfully sourced the desired olm.bundle content",
	})

	if err := r.Applier.Apply(ctx, po, desiredBundle); err != nil {
		meta.SetStatusCondition(&po.Status.Conditions, metav1.Condition{
			Type:    platformv1alpha1.TypeApplied,
			Status:  metav1.ConditionUnknown,
			Reason:  platformv1alpha1.ReasonApplyFailed,
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}
	meta.SetStatusCondition(&po.Status.Conditions, metav1.Condition{
		Type:    platformv1alpha1.TypeApplied,
		Status:  metav1.ConditionTrue,
		Reason:  platformv1alpha1.ReasonApplySuccessful,
		Message: "Successfully applied the desired olm.bundle content",
	})
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PlatformOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.PlatformOperator{}).
		Watches(&source.Kind{Type: &operatorsv1alpha1.CatalogSource{}}, handler.EnqueueRequestsFromMapFunc(util.RequeuePlatformOperators(mgr.GetClient()))).
		Watches(&source.Kind{Type: &rukpakv1alpha1.BundleInstance{}}, handler.EnqueueRequestsFromMapFunc(util.RequeueBundleInstance(mgr.GetClient()))).
		Complete(r)
}
