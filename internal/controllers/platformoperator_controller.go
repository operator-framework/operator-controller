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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	platformv1alpha1 "github.com/openshift/api/platform/v1alpha1"
	platformtypes "github.com/timflannagan/platform-operators/api/v1alpha1"
	"github.com/timflannagan/platform-operators/internal/sourcer"
)

// PlatformOperatorReconciler reconciles a PlatformOperator object
type PlatformOperatorReconciler struct {
	client.Client
	Sourcer sourcer.Sourcer
}

//+kubebuilder:rbac:groups=platform.openshift.io,resources=platformoperators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=platform.openshift.io,resources=platformoperators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=platform.openshift.io,resources=platformoperators/finalizers,verbs=update
//+kubebuilder:rbac:groups=core.olm.io,resources=operators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.olm.io,resources=operators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.olm.io,resources=operators/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *PlatformOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logr.FromContext(ctx)
	log.Info("reconciling request", "req", req.NamespacedName)
	defer log.Info("finished reconciling request", "req", req.NamespacedName)

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

	// ensure an Operator resource is created under-the-hood.
	if err := r.ensureOperator(ctx, po); err != nil {
		meta.SetStatusCondition(&po.Status.Conditions, metav1.Condition{
			Type:    platformtypes.TypeInstalled,
			Status:  metav1.ConditionFalse,
			Reason:  platformtypes.ReasonInstallFailed,
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}

	// TODO(tflannag): Bubble up underlying Operator resource status.
	meta.SetStatusCondition(&po.Status.Conditions, metav1.Condition{
		Type:    platformtypes.TypeInstalled,
		Status:  metav1.ConditionTrue,
		Reason:  platformtypes.ReasonInstallSuccessful,
		Message: fmt.Sprintf("Successfully applied the %s Operator resource", po.GetName()),
	})

	return ctrl.Result{}, nil
}

func (r *PlatformOperatorReconciler) ensureOperator(ctx context.Context, po *platformv1alpha1.PlatformOperator) error {
	o := &platformtypes.Operator{}
	o.SetName(po.GetName())
	controllerRef := metav1.NewControllerRef(po, po.GroupVersionKind())

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, o, func() error {
		o.SetOwnerReferences([]metav1.OwnerReference{*controllerRef})
		o.Spec = platformtypes.OperatorSpec{
			// Note(tflannag): Hardcode this for now given it's a phase 0
			// restriction that will ease over time. We don't have a good
			// idea on what an installer UX configuration will look like,
			// so this should be sufficient for now.
			Catalog: &platformtypes.CatalogSpec{
				Name:      "redhat-operators",
				Namespace: "openshift-marketplace",
			},
			Package: &platformtypes.PackageSpec{
				Name: po.Spec.Package.Name,
			},
		}
		return nil
	})
	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *PlatformOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.PlatformOperator{}).
		Watches(&source.Kind{Type: &platformtypes.Operator{}}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &platformv1alpha1.PlatformOperator{},
		}).
		Complete(r)
}
