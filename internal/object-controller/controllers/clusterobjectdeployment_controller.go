//go:build !standard

package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// ClusterObjectDeploymentReconciler reconciles ClusterObjectDeployment objects by
// managing their owned ClusterObjectSet revisions.
//
// This is a skeleton: it does not yet implement any reconciliation behavior.
type ClusterObjectDeploymentReconciler struct {
	Client client.Client
}

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterobjectdeployments,verbs=get;list;watch;update;patch;create;delete
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterobjectdeployments/status,verbs=update;patch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterobjectdeployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterobjectsets,verbs=get;list;watch;update;patch;create;delete

func (c *ClusterObjectDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("cluster-object-deployment")
	ctx = log.IntoContext(ctx, l)

	existingDeployment := &ocv1.ClusterObjectDeployment{}
	if err := c.Client.Get(ctx, req.NamespacedName, existingDeployment); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	l.Info("reconcile starting")
	defer l.Info("reconcile ending")

	// No reconciliation behavior yet. This skeleton establishes the controller,
	// its watches, and its RBAC so that behavior can be layered on in follow-ups.

	return ctrl.Result{}, nil
}

func (c *ClusterObjectDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(
			&ocv1.ClusterObjectDeployment{},
			builder.WithPredicates(
				predicate.ResourceVersionChangedPredicate{},
			),
		).
		Owns(
			&ocv1.ClusterObjectSet{},
			builder.WithPredicates(
				predicate.ResourceVersionChangedPredicate{},
			),
		).
		// A ClusterObjectSet's spec.group matches the name of the ClusterObjectDeployment
		// that owns (or should adopt) it. Owns() only enqueues already-owned revisions, so
		// this watch additionally surfaces not-yet-adopted (orphan) ClusterObjectSets.
		Watches(
			&ocv1.ClusterObjectSet{},
			handler.EnqueueRequestsFromMapFunc(mapClusterObjectSetToClusterObjectDeployment),
		).
		Complete(c)
}

// mapClusterObjectSetToClusterObjectDeployment enqueues the ClusterObjectDeployment
// named by a ClusterObjectSet's spec.group.
func mapClusterObjectSetToClusterObjectDeployment(_ context.Context, obj client.Object) []ctrl.Request {
	cos, ok := obj.(*ocv1.ClusterObjectSet)
	if !ok || cos.Spec.Group == "" {
		return nil
	}
	return []ctrl.Request{{NamespacedName: types.NamespacedName{Name: cos.Spec.Group}}}
}
