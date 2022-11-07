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
	"fmt"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	platformtypes "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/applier"
	"github.com/operator-framework/operator-controller/internal/sourcer"
	"github.com/operator-framework/operator-controller/internal/util"
)

// OperatorReconciler reconciles an Operator object
type OperatorReconciler struct {
	client.Client
	sourcer.Sourcer
}

//+kubebuilder:rbac:groups=core.olm.io,resources=operators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.olm.io,resources=operators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.olm.io,resources=operators/finalizers,verbs=update
//+kubebuilder:rbac:groups=operators.coreos.com,resources=catalogsources,verbs=get;list;watch
//+kubebuilder:rbac:groups=core.rukpak.io,resources=bundledeployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.rukpak.io,resources=bundles,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *OperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logr.FromContext(ctx)
	log.Info("reconciling request", "req", req.NamespacedName)
	defer log.Info("finished reconciling request", "req", req.NamespacedName)

	o := &platformtypes.Operator{}
	if err := r.Get(ctx, req.NamespacedName, o); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	defer func() {
		o := o.DeepCopy()
		o.ObjectMeta.ManagedFields = nil
		if err := r.Status().Patch(ctx, o, client.Apply, client.FieldOwner("platformoperator")); err != nil {
			log.Error(err, "failed to patch status")
		}
	}()

	source, err := r.Source(ctx, o)
	if err != nil {
		meta.SetStatusCondition(&o.Status.Conditions, metav1.Condition{
			Type:    platformtypes.TypeInstalled,
			Status:  metav1.ConditionFalse,
			Reason:  platformtypes.ReasonSourceFailed,
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}
	platformtypes.SetSourceInfo(o, platformtypes.SourceInfo{
		Name:      source.SourceInfo.Name,
		Namespace: source.SourceInfo.Namespace,
		Version:   source.Version,
	})

	bd, err := applier.Apply(ctx, o, r.Client, *source)
	if err != nil {
		meta.SetStatusCondition(&o.Status.Conditions, metav1.Condition{
			Type:    platformtypes.TypeInstalled,
			Status:  metav1.ConditionFalse,
			Reason:  platformtypes.ReasonInstallFailed,
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}
	platformtypes.SetActiveBundleDeployment(o, bd.GetName())

	// check whether the generated BundleDeployment are reporting any
	// failures when attempting to unpack the configured registry+v1
	// bundle contents, or persisting those unpack contents to the cluster.
	if failureCond := util.InspectBundleDeployment(ctx, bd.Status.Conditions); failureCond != nil {
		// avoid returning an error here as the controller is watching for BD resource
		// events. this should avoid unnecessary requeues when the BD is still in the
		// same state.
		meta.SetStatusCondition(&o.Status.Conditions, *failureCond)
		return ctrl.Result{}, nil
	}
	meta.SetStatusCondition(&o.Status.Conditions, metav1.Condition{
		Type:    platformtypes.TypeInstalled,
		Status:  metav1.ConditionTrue,
		Reason:  platformtypes.ReasonInstallSuccessful,
		Message: fmt.Sprintf("Successfully applied the %s BundleDeployment resource", bd.GetName()),
	})

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformtypes.Operator{}).
		Watches(&source.Kind{Type: &operatorsv1alpha1.CatalogSource{}}, handler.EnqueueRequestsFromMapFunc(util.RequeueOperators(mgr.GetClient()))).
		Watches(&source.Kind{Type: &rukpakv1alpha1.BundleDeployment{}}, handler.EnqueueRequestsFromMapFunc(util.RequeueBundleDeployment(mgr.GetClient()))).
		Complete(r)
}
