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
	"errors"
	"fmt"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	platformtypes "github.com/timflannagan/platform-operators/api/v1alpha1"
	"github.com/timflannagan/platform-operators/internal/applier"
	"github.com/timflannagan/platform-operators/internal/sourcer"
	"github.com/timflannagan/platform-operators/internal/util"
)

var (
	errSourceFailed = errors.New("failed to run sourcing logic")
)

// OperatorReconciler reconciles an Operator object
type OperatorReconciler struct {
	client.Client
	Sourcer sourcer.Sourcer
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

	bd, err := r.ensureDesiredBundleDeployment(ctx, o)
	if err != nil {
		// check whether we failed to return an active BundleDeployment
		// resource due to sourcing failures. These sourcing failures are
		// possible if the desired package name isn't present in the supported
		// catalog sources in the cluster.
		reason := platformtypes.ReasonInstallFailed
		if errors.Is(err, errSourceFailed) {
			reason = platformtypes.ReasonSourceFailed
		}
		meta.SetStatusCondition(&o.Status.Conditions, metav1.Condition{
			Type:    platformtypes.TypeInstalled,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
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

func (r *OperatorReconciler) ensureDesiredBundleDeployment(ctx context.Context, o *platformtypes.Operator) (*rukpakv1alpha1.BundleDeployment, error) {
	bd := &rukpakv1alpha1.BundleDeployment{}

	// check whether the underlying BD has already been generated to determine
	// whether the sourcing logic needs to be run to avoid performing unnecessary
	// work given upgrades aren't supported during phase 0. Note: this logic
	// doesn't compare the current and desired status of the BD resource so it's
	// possible that users/controllers/etc. can modify the generated BD resource.
	// See https://github.com/timflannagan/platform-operators/issues/47 for more details.
	if err := r.Get(ctx, types.NamespacedName{Name: o.GetName()}, bd); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}
		// TODO: surface the sourced bundle version in the Operator's status.
		sourcedBundle, err := r.Sourcer.Source(ctx, o)
		if err != nil {
			return nil, fmt.Errorf("%v: %w", err, errSourceFailed)
		}

		// TODO: Remove this debug artifact once upgrades are better supported,
		// and this overall logic is refactored.
		log := logr.FromContext(ctx)
		log.Info("successfully sourced a registry+v1 bundle", "version", sourcedBundle.Version)

		bd = applier.NewBundleDeployment(o, sourcedBundle.Image)
		if err := r.Create(ctx, bd); err != nil {
			return nil, err
		}
	}
	return bd, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformtypes.Operator{}).
		Watches(&source.Kind{Type: &operatorsv1alpha1.CatalogSource{}}, handler.EnqueueRequestsFromMapFunc(util.RequeueOperators(mgr.GetClient()))).
		Watches(&source.Kind{Type: &rukpakv1alpha1.BundleDeployment{}}, handler.EnqueueRequestsFromMapFunc(util.RequeueBundleDeployment(mgr.GetClient()))).
		Complete(r)
}
