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

	configv1 "github.com/openshift/api/config/v1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	platformv1alpha1 "github.com/openshift/api/platform/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/clusteroperator"
	"github.com/operator-framework/operator-controller/internal/util"
)

type AggregatedClusterOperatorReconciler struct {
	client.Client
	ReleaseVersion  string
	SystemNamespace string
}

//+kubebuilder:rbac:groups=platform.openshift.io,resources=platformoperators,verbs=list
//+kubebuilder:rbac:groups=config.openshift.io,resources=clusteroperators,verbs=get;list;watch
//+kubebuilder:rbac:groups=config.openshift.io,resources=clusteroperators/status,verbs=update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *AggregatedClusterOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logr.FromContext(ctx)
	log.Info("reconciling request", "req", req.NamespacedName)
	defer log.Info("finished reconciling request", "req", req.NamespacedName)

	coBuilder := clusteroperator.NewBuilder()
	coWriter := clusteroperator.NewWriter(r.Client)

	aggregatedCO := &configv1.ClusterOperator{}
	if err := r.Get(ctx, req.NamespacedName, aggregatedCO); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	defer func() {
		if err := coWriter.UpdateStatus(ctx, aggregatedCO, coBuilder.GetStatus()); err != nil {
			log.Error(err, "error updating cluster operator status")
		}
	}()

	// Set the default CO status conditions: Progressing=True, Degraded=False, Available=False
	// TODO: always set a reason (message is optional, but desirable)
	coBuilder.WithProgressing(metav1.ConditionTrue, "", "")
	coBuilder.WithDegraded(metav1.ConditionFalse, "", "")
	coBuilder.WithAvailable(metav1.ConditionFalse, "", "")
	coBuilder.WithVersion("operator", r.ReleaseVersion)

	// Set the static set of related objects
	setStaticRelatedObjects(coBuilder, r.SystemNamespace)

	poList := &platformv1alpha1.PlatformOperatorList{}
	if err := r.List(ctx, poList); err != nil {
		return ctrl.Result{}, err
	}
	if len(poList.Items) == 0 {
		// No POs on cluster, everything is fine
		coBuilder.WithAvailable(metav1.ConditionTrue, clusteroperator.ReasonAsExpected, "No platform operators are present in the cluster")
		coBuilder.WithProgressing(metav1.ConditionFalse, clusteroperator.ReasonAsExpected, "No platform operators are present in the cluster")
		return ctrl.Result{}, nil
	}

	// check whether any of the underlying PO resources are reporting
	// any failing status states, and update the aggregate CO resource
	// to reflect those failing PO resources.

	// TODO: consider something more fine-grained than a catch-all "PlatformOperatorError" reason.
	//   There's a non-negligible difference between "PO is explicitly failing installation" and "PO is not yet installed"
	if statusErrorCheck := util.InspectPlatformOperators(poList); statusErrorCheck != nil {
		coBuilder.WithAvailable(metav1.ConditionFalse, clusteroperator.ReasonPlatformOperatorError, statusErrorCheck.Error())
		return ctrl.Result{}, nil
	}
	coBuilder.WithAvailable(metav1.ConditionTrue, clusteroperator.ReasonAsExpected, "All platform operators are in a successful state")
	coBuilder.WithProgressing(metav1.ConditionFalse, clusteroperator.ReasonAsExpected, "All platform operators are in a successful state")

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AggregatedClusterOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1.ClusterOperator{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
			return object.GetName() == clusteroperator.AggregateResourceName
		}))).
		Watches(&source.Kind{Type: &platformv1alpha1.PlatformOperator{}}, handler.EnqueueRequestsFromMapFunc(util.RequeueClusterOperator(mgr.GetClient(), clusteroperator.AggregateResourceName))).
		Complete(r)
}

func setStaticRelatedObjects(coBuilder *clusteroperator.Builder, systemNamespace string) {
	coBuilder.
		WithRelatedObject(configv1.ObjectReference{Group: "", Resource: "namespaces", Name: systemNamespace}).

		// NOTE: Group and resource can be referenced without name/namespace set, which is a signal
		// that _ALL_ objects of that group/resource are related objects. This is useful for
		// must-gather automation.
		WithRelatedObject(configv1.ObjectReference{Group: platformv1alpha1.GroupName, Resource: "platformoperators"}).

		// TODO: move platform operator ownership of rukpak objects out prior to rukpak or PO GA.
		WithRelatedObject(configv1.ObjectReference{Group: rukpakv1alpha1.GroupVersion.Group, Resource: "bundles"}).
		WithRelatedObject(configv1.ObjectReference{Group: rukpakv1alpha1.GroupVersion.Group, Resource: "bundledeployments"})
}
