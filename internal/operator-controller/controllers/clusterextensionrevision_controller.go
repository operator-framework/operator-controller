//go:build experimental

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/utils/ptr"
	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/managedcache"
	"pkg.package-operator.run/boxcutter/ownerhandling"
	"pkg.package-operator.run/boxcutter/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	ClusterExtensionRevisionOwnerLabel = "olm.operatorframework.io/owner"

	boxcutterSystemPrefixFieldOwner           = "olm.operatorframework.io"
	clusterExtensionRevisionTeardownFinalizer = "olm.operatorframework.io/teardown"
)

// ClusterExtensionRevisionReconciler actions individual snapshots of ClusterExtensions,
// as part of the boxcutter integration.
type ClusterExtensionRevisionReconciler struct {
	Client          client.Client
	AccessManager   accessManager
	Scheme          *runtime.Scheme
	RestMapper      meta.RESTMapper
	DiscoveryClient *discovery.DiscoveryClient
}

type accessManager interface {
	GetWithUser(
		ctx context.Context, owner *ocv1.ClusterExtension,
		user client.Object, usedFor []client.Object,
	) (managedcache.Accessor, error)
	FreeWithUser(ctx context.Context, owner *ocv1.ClusterExtension, user client.Object) error
	Source(handler.EventHandler, ...predicate.Predicate) source.Source
}

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensionrevisions,verbs=get;list;watch;update;patch;create;delete
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensionrevisions/status,verbs=update;patch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensionrevisions/finalizers,verbs=update

func (c *ClusterExtensionRevisionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	l := log.FromContext(ctx).WithName("cluster-extension-revision")
	ctx = log.IntoContext(ctx, l)

	rev := &ocv1.ClusterExtensionRevision{}
	if err := c.Client.Get(
		ctx, req.NamespacedName, rev); err != nil {
		return res, client.IgnoreNotFound(err)
	}

	l = l.WithValues("key", req.NamespacedName.String())
	l.Info("reconcile starting")
	defer l.Info("reconcile ending")

	controller, ok := getControllingClusterExtension(rev)
	if !ok {
		// ClusterExtension revisions can't exist without a ClusterExtension in control.
		// This situation can only appear if the ClusterExtension object has been deleted with --cascade=Orphan.
		// To not leave unactionable resources on the cluster, we are going to just
		// reap the revision reverences and propagate the Orphan deletion.
		if rev.DeletionTimestamp.IsZero() {
			err := client.IgnoreNotFound(
				c.Client.Delete(ctx, rev, client.PropagationPolicy(metav1.DeletePropagationOrphan), client.Preconditions{
					UID:             ptr.To(rev.GetUID()),
					ResourceVersion: ptr.To(rev.GetResourceVersion()),
				}),
			)
			if err != nil {
				return res, err
			}
			// we get requeued to remove the finalizer.
			return res, nil
		}
		if err := c.removeFinalizer(ctx, rev, clusterExtensionRevisionTeardownFinalizer); err != nil {
			return res, err
		}
		return res, nil
	}

	return c.reconcile(ctx, controller, rev)
}

func (c *ClusterExtensionRevisionReconciler) reconcile(
	ctx context.Context, ce *ocv1.ClusterExtension, rev *ocv1.ClusterExtensionRevision,
) (res ctrl.Result, err error) {
	l := log.FromContext(ctx)

	revision, opts, previous, err := toBoxcutterRevision(ce.Name, rev)
	if err != nil {
		return res, fmt.Errorf("converting CM to revision: %w", err)
	}

	var objects []client.Object
	for _, phase := range revision.GetPhases() {
		for _, pobj := range phase.GetObjects() {
			objects = append(objects, &pobj)
		}
	}

	// THIS IS STUPID, PLEASE FIX!
	// Revisions need individual finalizers on the ClusterExtension to prevent it's premature deletion.
	if rev.DeletionTimestamp.IsZero() &&
		rev.Spec.LifecycleState != ocv1.ClusterExtensionRevisionLifecycleStateArchived {
		// We can't lookup the complete ClusterExtension when it's already deleted.
		// This only works when the controller-manager is not restarted during teardown.
		if err := c.Client.Get(ctx, client.ObjectKeyFromObject(ce), ce); err != nil {
			return res, err
		}
	}

	accessor, err := c.AccessManager.GetWithUser(ctx, ce, rev, objects)
	if err != nil {
		return res, fmt.Errorf("get cache: %w", err)
	}

	// Boxcutter machinery setup.
	os := ownerhandling.NewNative(c.Scheme)
	pval := validation.NewClusterPhaseValidator(c.RestMapper, accessor)
	rval := validation.NewRevisionValidator()
	comp := machinery.NewComparator(
		os, c.DiscoveryClient, c.Scheme, boxcutterSystemPrefixFieldOwner)
	oe := machinery.NewObjectEngine(
		c.Scheme, accessor, accessor,
		os, comp, boxcutterSystemPrefixFieldOwner, boxcutterSystemPrefixFieldOwner,
	)
	pe := machinery.NewPhaseEngine(oe, pval)
	re := machinery.NewRevisionEngine(pe, rval, accessor)

	if !rev.DeletionTimestamp.IsZero() ||
		rev.Spec.LifecycleState == ocv1.ClusterExtensionRevisionLifecycleStateArchived {
		//
		// Teardown
		//
		tres, err := re.Teardown(ctx, *revision)
		if err != nil {
			return res, fmt.Errorf("revision teardown: %w", err)
		}

		l.Info("teardown report", "report", tres.String())

		if !tres.IsComplete() {
			return res, nil
		}
		if err := c.AccessManager.FreeWithUser(ctx, ce, rev); err != nil {
			return res, fmt.Errorf("get cache: %w", err)
		}
		if err := c.removeFinalizer(ctx, rev, clusterExtensionRevisionTeardownFinalizer); err != nil {
			return res, err
		}
		return res, nil
	}

	//
	// Reconcile
	//
	if err := c.ensureFinalizer(ctx, rev, clusterExtensionRevisionTeardownFinalizer); err != nil {
		return res, err
	}
	rres, err := re.Reconcile(ctx, *revision, opts...)
	if err != nil {
		return res, fmt.Errorf("revision reconcile: %w", err)
	}
	l.Info("reconcile report", "report", rres.String())

	// Retry failing preflight checks with a flat 10s retry.
	// TODO: report status, backoff?
	if verr := rres.GetValidationError(); verr != nil {
		l.Info("preflight error, retrying after 10s", "err", verr.String())

		res.RequeueAfter = 10 * time.Second
		//nolint:nilerr
		return res, nil
	}
	for _, pres := range rres.GetPhases() {
		if verr := pres.GetValidationError(); verr != nil {
			l.Info("preflight error, retrying after 10s", "err", verr.String())

			res.RequeueAfter = 10 * time.Second
			//nolint:nilerr
			return res, nil
		}
	}

	if rres.IsComplete() {
		// Archive other revisions.
		for _, a := range previous {
			if err := c.Client.Patch(ctx, a, client.RawPatch(
				types.MergePatchType, []byte(`{"data":{"state":"Archived"}}`))); err != nil {
				return res, fmt.Errorf("archive previous Revision: %w", err)
			}
		}

		// Report status.
		meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
			Type:               "Available",
			Status:             metav1.ConditionTrue,
			Reason:             "Available",
			Message:            "Object is available and passes all probes.",
			ObservedGeneration: rev.Generation,
		})
		if !meta.IsStatusConditionTrue(rev.Status.Conditions, "Succeeded") {
			meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
				Type:               "Succeeded",
				Status:             metav1.ConditionTrue,
				Reason:             "RolloutSuccess",
				Message:            "Revision succeeded rolling out.",
				ObservedGeneration: rev.Generation,
			})
		}
	} else {
		var probeFailureMsgs []string
		for _, pres := range rres.GetPhases() {
			if pres.IsComplete() {
				continue
			}
			for _, ores := range pres.GetObjects() {
				pr := ores.Probes()[boxcutter.ProgressProbeType]
				if pr.Success {
					continue
				}

				obj := ores.Object()
				gvk := obj.GetObjectKind().GroupVersionKind()
				probeFailureMsgs = append(probeFailureMsgs, fmt.Sprintf(
					"Object %s.%s %s/%s: %v",
					gvk.Kind, gvk.GroupVersion().String(),
					obj.GetNamespace(), obj.GetName(), strings.Join(pr.Messages, " and "),
				))
				break
			}
		}
		if len(probeFailureMsgs) > 0 {
			meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
				Type:               "Available",
				Status:             metav1.ConditionFalse,
				Reason:             "ProbeFailure",
				Message:            strings.Join(probeFailureMsgs, "\n"),
				ObservedGeneration: rev.Generation,
			})
		} else {
			meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
				Type:               "Available",
				Status:             metav1.ConditionFalse,
				Reason:             "Incomplete",
				Message:            "Revision has not been rolled out completely.",
				ObservedGeneration: rev.Generation,
			})
		}
	}
	if rres.InTransistion() {
		meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
			Type:               "InTransition",
			Status:             metav1.ConditionTrue,
			Reason:             "InTransition",
			Message:            "Rollout in progress.",
			ObservedGeneration: rev.Generation,
		})
	} else {
		meta.RemoveStatusCondition(&rev.Status.Conditions, "InTransition")
	}

	return res, c.Client.Status().Update(ctx, rev)
}

func (c *ClusterExtensionRevisionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(
			&ocv1.ClusterExtensionRevision{},
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		WatchesRawSource(
			c.AccessManager.Source(
				handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &ocv1.ClusterExtensionRevision{}),
				predicate.ResourceVersionChangedPredicate{},
			),
		).
		Complete(c)
}

func (c *ClusterExtensionRevisionReconciler) ensureFinalizer(
	ctx context.Context, obj client.Object, finalizer string,
) error {
	if controllerutil.ContainsFinalizer(obj, finalizer) {
		return nil
	}

	controllerutil.AddFinalizer(obj, finalizer)
	patch := map[string]any{
		"metadata": map[string]any{
			"resourceVersion": obj.GetResourceVersion(),
			"finalizers":      obj.GetFinalizers(),
		},
	}
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshalling patch to remove finalizer: %w", err)
	}
	if err := c.Client.Patch(ctx, obj, client.RawPatch(types.MergePatchType, patchJSON)); err != nil {
		return fmt.Errorf("adding finalizer: %w", err)
	}
	return nil
}

func (c *ClusterExtensionRevisionReconciler) removeFinalizer(
	ctx context.Context, obj client.Object, finalizer string,
) error {
	if !controllerutil.ContainsFinalizer(obj, finalizer) {
		return nil
	}

	controllerutil.RemoveFinalizer(obj, finalizer)

	patch := map[string]any{
		"metadata": map[string]any{
			"resourceVersion": obj.GetResourceVersion(),
			"finalizers":      obj.GetFinalizers(),
		},
	}
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshalling patch to remove finalizer: %w", err)
	}
	if err := c.Client.Patch(ctx, obj, client.RawPatch(types.MergePatchType, patchJSON)); err != nil {
		return fmt.Errorf("removing finalizer: %w", err)
	}
	return nil
}

// getControllingClusterExtension checks the objects ownerreferences for a ClusterExtension
// with the controller flag set to true.
// Returns a ClusterExtension with metadata recovered from the OwnerRef or nil.
func getControllingClusterExtension(obj client.Object) (*ocv1.ClusterExtension, bool) {
	for _, v := range obj.GetOwnerReferences() {
		if v.Controller != nil && *v.Controller &&
			v.APIVersion == ocv1.GroupVersion.String() &&
			v.Kind == "ClusterExtension" {
			return &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					UID:  v.UID,
					Name: v.Name,
				},
			}, true
		}
	}
	return nil, false
}

func toBoxcutterRevision(clusterExtensionName string, rev *ocv1.ClusterExtensionRevision) (
	r *boxcutter.Revision, opts []boxcutter.RevisionReconcileOption, previous []client.Object, err error,
) {
	r = &boxcutter.Revision{
		Name:     rev.Name,
		Owner:    rev,
		Revision: rev.Spec.Revision,
	}
	for _, specPhase := range rev.Spec.Phases {
		phase := boxcutter.Phase{Name: specPhase.Name}
		for _, specObj := range specPhase.Objects {
			obj := specObj.Object

			labels := obj.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
			labels[ClusterExtensionRevisionOwnerLabel] = clusterExtensionName
			obj.SetLabels(labels)

			phase.Objects = append(phase.Objects, obj)
		}
		r.Phases = append(r.Phases, phase)
	}

	for _, specPrevious := range rev.Spec.Previous {
		prev := &unstructured.Unstructured{}
		prev.SetName(specPrevious.Name)
		prev.SetUID(specPrevious.UID)
		prev.SetGroupVersionKind(ocv1.GroupVersion.WithKind(ocv1.ClusterExtensionRevisionKind))
		previous = append(previous, prev)
	}

	opts = []boxcutter.RevisionReconcileOption{
		boxcutter.WithPreviousOwners(previous),
		boxcutter.WithProbe(boxcutter.ProgressProbeType, boxcutter.ProbeFunc(func(obj client.Object) (success bool, messages []string) {
			deployGK := schema.GroupKind{
				Group: "apps", Kind: "Deployment",
			}
			if obj.GetObjectKind().GroupVersionKind().GroupKind() != deployGK {
				return true, nil
			}
			ustrObj := obj.(*unstructured.Unstructured)
			depl := &appsv1.Deployment{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(ustrObj.Object, depl); err != nil {
				return false, []string{err.Error()}
			}

			if depl.Status.ObservedGeneration != depl.Generation {
				return false, []string{".status.observedGeneration outdated"}
			}
			for _, cond := range depl.Status.Conditions {
				if cond.Type == "Available" &&
					cond.Status == corev1.ConditionTrue &&
					depl.Status.UpdatedReplicas == *depl.Spec.Replicas {
					return true, nil
				}
			}

			return false, []string{"not available or not fully updated"}
		})),
	}
	if rev.Spec.LifecycleState == ocv1.ClusterExtensionRevisionLifecycleStatePaused {
		opts = append(opts, boxcutter.WithPaused{})
	}
	return
}
