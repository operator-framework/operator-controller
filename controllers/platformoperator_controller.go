package controllers

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	deppyv1alpha1 "github.com/operator-framework/deppy/api/v1alpha1"
	platformv1alpha1 "github.com/timflannagan/platform-operators/api/v1alpha1"
	"github.com/timflannagan/platform-operators/internal/util"
)

// PlatformOperatorReconciler reconciles a PlatformOperator object
type PlatformOperatorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	packageConstraintType = "olm.RequirePackage"
	packageValueKey       = "package"
)

//+kubebuilder:rbac:groups=platform.openshift.io,resources=platformoperators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=platform.openshift.io,resources=platformoperators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=platform.openshift.io,resources=platformoperators/finalizers,verbs=update
//+kubebuilder:rbac:groups=core.deppy.io,resources=resolutions,verbs=get;list;watch;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
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
	if err := r.ensureResolution(ctx, po); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *PlatformOperatorReconciler) ensureResolution(ctx context.Context, po *platformv1alpha1.PlatformOperator) error {
	res := &deppyv1alpha1.Resolution{}
	res.SetName(po.GetName())
	controllerRef := metav1.NewControllerRef(po, po.GroupVersionKind())

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, res, func() error {
		res.SetOwnerReferences([]metav1.OwnerReference{*controllerRef})
		desiredPackages := make([]deppyv1alpha1.Constraint, 0)
		for _, name := range po.Spec.Packages {
			desiredPackages = append(desiredPackages, newPackageRequirement(name))
		}
		// TODO: sort these packages to ensure determinism
		res.Spec.Constraints = desiredPackages
		return nil
	})
	return err
}

func newPackageRequirement(packageName string) deppyv1alpha1.Constraint {
	return deppyv1alpha1.Constraint{
		Type: packageConstraintType,
		Value: map[string]string{
			packageValueKey: packageName,
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PlatformOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.PlatformOperator{}).
		Watches(&source.Kind{Type: &deppyv1alpha1.Resolution{}}, handler.EnqueueRequestsFromMapFunc(util.RequeuePlatformOperators(r.Client))).
		Complete(r)
}
