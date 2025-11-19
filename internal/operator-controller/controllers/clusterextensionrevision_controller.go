//go:build !standard

package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/machinery"
	machinerytypes "pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/probing"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

const (
	clusterExtensionRevisionTeardownFinalizer = "olm.operatorframework.io/teardown"
)

// ClusterExtensionRevisionReconciler actions individual snapshots of ClusterExtensions,
// as part of the boxcutter integration.
type ClusterExtensionRevisionReconciler struct {
	Client         client.Client
	RevisionEngine RevisionEngine
	TrackingCache  trackingCache
}

type trackingCache interface {
	client.Reader
	Source(handler handler.EventHandler, predicates ...predicate.Predicate) source.Source
	Watch(ctx context.Context, user client.Object, gvks sets.Set[schema.GroupVersionKind]) error
	Free(ctx context.Context, user client.Object) error
}

type RevisionEngine interface {
	Teardown(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionTeardownOption) (machinery.RevisionTeardownResult, error)
	Reconcile(ctx context.Context, rev machinerytypes.Revision, opts ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error)
}

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensionrevisions,verbs=get;list;watch;update;patch;create;delete
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensionrevisions/status,verbs=update;patch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterextensionrevisions/finalizers,verbs=update

func (c *ClusterExtensionRevisionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("cluster-extension-revision")
	ctx = log.IntoContext(ctx, l)

	existingRev := &ocv1.ClusterExtensionRevision{}
	if err := c.Client.Get(ctx, req.NamespacedName, existingRev); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	l.Info("reconcile starting")
	defer l.Info("reconcile ending")

	reconciledRev := existingRev.DeepCopy()
	res, reconcileErr := c.reconcile(ctx, reconciledRev)

	// Do checks before any Update()s, as Update() may modify the resource structure!
	updateStatus := !equality.Semantic.DeepEqual(existingRev.Status, reconciledRev.Status)

	unexpectedFieldsChanged := checkForUnexpectedClusterExtensionRevisionFieldChange(*existingRev, *reconciledRev)
	if unexpectedFieldsChanged {
		panic("spec or metadata changed by reconciler")
	}

	// NOTE: finalizer updates are performed during c.reconcile as patches, so that reconcile can
	//   continue performing logic after successfully setting the finalizer. therefore we only need
	//   to set status here.

	if updateStatus {
		if err := c.Client.Status().Update(ctx, reconciledRev); err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("error updating status: %v", err))
		}
	}

	return res, reconcileErr
}

// Compare resources - ignoring status & metadata.finalizers
func checkForUnexpectedClusterExtensionRevisionFieldChange(a, b ocv1.ClusterExtensionRevision) bool {
	a.Status, b.Status = ocv1.ClusterExtensionRevisionStatus{}, ocv1.ClusterExtensionRevisionStatus{}

	// when finalizers are updated during reconcile, we expect finalizers, managedFields, and resourceVersion
	// to be updated, so we ignore changes in these fields.
	a.Finalizers, b.Finalizers = []string{}, []string{}
	a.ManagedFields, b.ManagedFields = nil, nil
	a.ResourceVersion, b.ResourceVersion = "", ""
	return !equality.Semantic.DeepEqual(a.Spec, b.Spec)
}

func (c *ClusterExtensionRevisionReconciler) reconcile(ctx context.Context, rev *ocv1.ClusterExtensionRevision) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	revision, opts, err := c.toBoxcutterRevision(ctx, rev)
	if err != nil {
		meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
			Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             ocv1.ClusterExtensionRevisionReasonReconcileFailure,
			Message:            err.Error(),
			ObservedGeneration: rev.Generation,
		})
		return ctrl.Result{}, fmt.Errorf("converting to boxcutter revision: %v", err)
	}

	if !rev.DeletionTimestamp.IsZero() || rev.Spec.LifecycleState == ocv1.ClusterExtensionRevisionLifecycleStateArchived {
		return c.teardown(ctx, rev, revision)
	}

	//
	// Reconcile
	//
	if err := c.ensureFinalizer(ctx, rev, clusterExtensionRevisionTeardownFinalizer); err != nil {
		meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
			Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             ocv1.ClusterExtensionRevisionReasonReconcileFailure,
			Message:            err.Error(),
			ObservedGeneration: rev.Generation,
		})
		return ctrl.Result{}, fmt.Errorf("error ensuring teardown finalizer: %v", err)
	}

	if err := c.establishWatch(ctx, rev, revision); err != nil {
		meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
			Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             ocv1.ClusterExtensionRevisionReasonReconcileFailure,
			Message:            err.Error(),
			ObservedGeneration: rev.Generation,
		})
		return ctrl.Result{}, fmt.Errorf("establish watch: %v", err)
	}

	rres, err := c.RevisionEngine.Reconcile(ctx, *revision, opts...)
	if err != nil {
		if rres != nil {
			l.Error(err, "revision reconcile failed")
			l.V(1).Info("reconcile failure report", "report", rres.String())
		} else {
			l.Error(err, "revision reconcile failed")
		}
		meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
			Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             ocv1.ClusterExtensionRevisionReasonReconcileFailure,
			Message:            err.Error(),
			ObservedGeneration: rev.Generation,
		})
		return ctrl.Result{}, fmt.Errorf("revision reconcile: %v", err)
	}
	// Log detailed reconcile reports only in debug mode (V(1)) to reduce verbosity.
	l.V(1).Info("reconcile report", "report", rres.String())

	// Retry failing preflight checks with a flat 10s retry.
	// TODO: report status, backoff?
	if verr := rres.GetValidationError(); verr != nil {
		l.Error(fmt.Errorf("%w", verr), "preflight validation failed, retrying after 10s")

		meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
			Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             ocv1.ClusterExtensionRevisionReasonRevisionValidationFailure,
			Message:            fmt.Sprintf("revision validation error: %s", verr),
			ObservedGeneration: rev.Generation,
		})
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	for i, pres := range rres.GetPhases() {
		if verr := pres.GetValidationError(); verr != nil {
			l.Error(fmt.Errorf("%w", verr), "phase preflight validation failed, retrying after 10s", "phase", i)

			meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
				Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
				Status:             metav1.ConditionFalse,
				Reason:             ocv1.ClusterExtensionRevisionReasonPhaseValidationError,
				Message:            fmt.Sprintf("phase %d validation error: %s", i, verr),
				ObservedGeneration: rev.Generation,
			})
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		var collidingObjs []string
		for _, ores := range pres.GetObjects() {
			if ores.Action() == machinery.ActionCollision {
				collidingObjs = append(collidingObjs, ores.String())
			}
		}

		if len(collidingObjs) > 0 {
			l.Error(fmt.Errorf("object collision detected"), "object collision, retrying after 10s", "phase", i, "collisions", collidingObjs)

			meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
				Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
				Status:             metav1.ConditionFalse,
				Reason:             ocv1.ClusterExtensionRevisionReasonObjectCollisions,
				Message:            fmt.Sprintf("revision object collisions in phase %d\n%s", i, strings.Join(collidingObjs, "\n\n")),
				ObservedGeneration: rev.Generation,
			})
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	//nolint:nestif
	if rres.IsComplete() {
		// Archive previous revisions
		previous, err := c.listPreviousRevisions(ctx, rev)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("listing previous revisions: %v", err)
		}
		for _, a := range previous {
			patch := []byte(`{"spec":{"lifecycleState":"Archived"}}`)
			if err := c.Client.Patch(ctx, client.Object(a), client.RawPatch(types.MergePatchType, patch)); err != nil {
				// TODO: It feels like an error here needs to propagate to a status _somewhere_.
				//   Not sure the current CER makes sense? But it also feels off to set the CE
				//   status from outside the CE reconciler.
				return ctrl.Result{}, fmt.Errorf("archive previous Revision: %w", err)
			}
		}

		// Report status.
		meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
			Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             ocv1.ClusterExtensionRevisionReasonAvailable,
			Message:            "Object is available and passes all probes.",
			ObservedGeneration: rev.Generation,
		})
		if !meta.IsStatusConditionTrue(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeSucceeded) {
			meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
				Type:               ocv1.ClusterExtensionRevisionTypeSucceeded,
				Status:             metav1.ConditionTrue,
				Reason:             ocv1.ClusterExtensionRevisionReasonRolloutSuccess,
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
				Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
				Status:             metav1.ConditionFalse,
				Reason:             ocv1.ClusterExtensionRevisionReasonProbeFailure,
				Message:            strings.Join(probeFailureMsgs, "\n"),
				ObservedGeneration: rev.Generation,
			})
		} else {
			meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
				Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
				Status:             metav1.ConditionFalse,
				Reason:             ocv1.ClusterExtensionRevisionReasonIncomplete,
				Message:            "Revision has not been rolled out completely.",
				ObservedGeneration: rev.Generation,
			})
		}
	}
	if rres.InTransistion() {
		meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
			Type:               ocv1.TypeProgressing,
			Status:             metav1.ConditionTrue,
			Reason:             ocv1.ClusterExtensionRevisionReasonProgressing,
			Message:            "Rollout in progress.",
			ObservedGeneration: rev.Generation,
		})
	} else {
		meta.RemoveStatusCondition(&rev.Status.Conditions, ocv1.TypeProgressing)
	}

	return ctrl.Result{}, nil
}

func (c *ClusterExtensionRevisionReconciler) teardown(ctx context.Context, rev *ocv1.ClusterExtensionRevision, revision *boxcutter.Revision) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	tres, err := c.RevisionEngine.Teardown(ctx, *revision)
	if err != nil {
		if tres != nil {
			l.Error(err, "revision teardown failed")
			l.V(1).Info("teardown failure report", "report", tres.String())
		} else {
			l.Error(err, "revision teardown failed")
		}
		meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
			Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             ocv1.ClusterExtensionRevisionReasonReconcileFailure,
			Message:            err.Error(),
			ObservedGeneration: rev.Generation,
		})
		return ctrl.Result{}, fmt.Errorf("revision teardown: %v", err)
	}

	// Log detailed teardown reports only in debug mode (V(1)) to reduce verbosity.
	l.V(1).Info("teardown report", "report", tres.String())
	if !tres.IsComplete() {
		// TODO: If it is not complete, it seems like it would be good to update
		//  the status in some way to tell the user that the teardown is still
		//  in progress.
		return ctrl.Result{}, nil
	}

	if err := c.TrackingCache.Free(ctx, rev); err != nil {
		meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
			Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             ocv1.ClusterExtensionRevisionReasonReconcileFailure,
			Message:            err.Error(),
			ObservedGeneration: rev.Generation,
		})
		return ctrl.Result{}, fmt.Errorf("error stopping informers: %v", err)
	}

	// Ensure Available condition is set to Unknown before removing the finalizer when archiving
	if rev.Spec.LifecycleState == ocv1.ClusterExtensionRevisionLifecycleStateArchived &&
		!meta.IsStatusConditionPresentAndEqual(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable, metav1.ConditionUnknown) {
		meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
			Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
			Status:             metav1.ConditionUnknown,
			Reason:             ocv1.ClusterExtensionRevisionReasonArchived,
			Message:            "revision is archived",
			ObservedGeneration: rev.Generation,
		})
		return ctrl.Result{}, nil
	}

	if err := c.removeFinalizer(ctx, rev, clusterExtensionRevisionTeardownFinalizer); err != nil {
		return ctrl.Result{}, fmt.Errorf("error removing teardown finalizer: %v", err)
	}
	return ctrl.Result{}, nil
}

type Sourcerer interface {
	Source(handler handler.EventHandler, predicates ...predicate.Predicate) source.Source
}

func (c *ClusterExtensionRevisionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(
			&ocv1.ClusterExtensionRevision{},
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		WatchesRawSource(
			c.TrackingCache.Source(
				handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &ocv1.ClusterExtensionRevision{}),
				predicate.ResourceVersionChangedPredicate{},
			),
		).
		Complete(c)
}

func (c *ClusterExtensionRevisionReconciler) establishWatch(
	ctx context.Context, rev *ocv1.ClusterExtensionRevision,
	boxcutterRev *boxcutter.Revision,
) error {
	gvks := sets.New[schema.GroupVersionKind]()
	for _, phase := range boxcutterRev.Phases {
		for _, obj := range phase.Objects {
			gvks.Insert(obj.GroupVersionKind())
		}
	}

	return c.TrackingCache.Watch(ctx, rev, gvks)
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

func (c *ClusterExtensionRevisionReconciler) removeFinalizer(ctx context.Context, obj client.Object, finalizer string) error {
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

// listPreviousRevisions returns active revisions belonging to the same ClusterExtension with lower revision numbers.
// Filters out the current revision, archived revisions, deleting revisions, and revisions with equal or higher numbers.
func (c *ClusterExtensionRevisionReconciler) listPreviousRevisions(ctx context.Context, rev *ocv1.ClusterExtensionRevision) ([]*ocv1.ClusterExtensionRevision, error) {
	ownerLabel, ok := rev.Labels[labels.OwnerNameKey]
	if !ok {
		// No owner label means this revision isn't properly labeled - return empty list
		return nil, nil
	}

	revList := &ocv1.ClusterExtensionRevisionList{}
	if err := c.TrackingCache.List(ctx, revList, client.MatchingLabels{
		labels.OwnerNameKey: ownerLabel,
	}); err != nil {
		return nil, fmt.Errorf("listing revisions: %w", err)
	}

	previous := make([]*ocv1.ClusterExtensionRevision, 0, len(revList.Items))
	for i := range revList.Items {
		r := &revList.Items[i]
		if r.Name == rev.Name {
			continue
		}
		// Skip archived or deleting revisions
		if r.Spec.LifecycleState == ocv1.ClusterExtensionRevisionLifecycleStateArchived ||
			!r.DeletionTimestamp.IsZero() {
			continue
		}
		// Only include revisions with lower revision numbers (actual previous revisions)
		if r.Spec.Revision >= rev.Spec.Revision {
			continue
		}
		previous = append(previous, r)
	}

	return previous, nil
}

func (c *ClusterExtensionRevisionReconciler) toBoxcutterRevision(ctx context.Context, rev *ocv1.ClusterExtensionRevision) (*boxcutter.Revision, []boxcutter.RevisionReconcileOption, error) {
	previous, err := c.listPreviousRevisions(ctx, rev)
	if err != nil {
		return nil, nil, fmt.Errorf("listing previous revisions: %w", err)
	}

	// Convert to []client.Object for boxcutter
	previousObjs := make([]client.Object, len(previous))
	for i, rev := range previous {
		previousObjs[i] = rev
	}

	opts := []boxcutter.RevisionReconcileOption{
		boxcutter.WithPreviousOwners(previousObjs),
		boxcutter.WithProbe(boxcutter.ProgressProbeType, probing.And{
			deploymentProbe, statefulSetProbe, crdProbe, issuerProbe, certProbe,
		}),
	}

	r := &boxcutter.Revision{
		Name:     rev.Name,
		Owner:    rev,
		Revision: rev.Spec.Revision,
	}
	for _, specPhase := range rev.Spec.Phases {
		phase := boxcutter.Phase{Name: specPhase.Name}
		for _, specObj := range specPhase.Objects {
			obj := specObj.Object.DeepCopy()

			objLabels := obj.GetLabels()
			if objLabels == nil {
				objLabels = map[string]string{}
			}
			objLabels[labels.OwnerNameKey] = rev.Labels[labels.OwnerNameKey]
			obj.SetLabels(objLabels)

			switch specObj.CollisionProtection {
			case ocv1.CollisionProtectionIfNoController, ocv1.CollisionProtectionNone:
				opts = append(opts, boxcutter.WithObjectReconcileOptions(
					obj, boxcutter.WithCollisionProtection(specObj.CollisionProtection)))
			}

			phase.Objects = append(phase.Objects, *obj)
		}
		r.Phases = append(r.Phases, phase)
	}

	if rev.Spec.LifecycleState == ocv1.ClusterExtensionRevisionLifecycleStatePaused {
		opts = append(opts, boxcutter.WithPaused{})
	}
	return r, opts, nil
}

var (
	deploymentProbe = &probing.GroupKindSelector{
		GroupKind: schema.GroupKind{Group: appsv1.GroupName, Kind: "Deployment"},
		Prober:    deplStatefulSetProbe,
	}
	statefulSetProbe = &probing.GroupKindSelector{
		GroupKind: schema.GroupKind{Group: appsv1.GroupName, Kind: "StatefulSet"},
		Prober:    deplStatefulSetProbe,
	}
	crdProbe = &probing.GroupKindSelector{
		GroupKind: schema.GroupKind{Group: "apiextensions.k8s.io", Kind: "CustomResourceDefinition"},
		Prober: &probing.ObservedGenerationProbe{
			Prober: &probing.ConditionProbe{ // "Available" == "True"
				Type:   string(apiextensions.Established),
				Status: string(corev1.ConditionTrue),
			},
		},
	}
	certProbe = &probing.GroupKindSelector{
		GroupKind: schema.GroupKind{Group: "acme.cert-manager.io", Kind: "Certificate"},
		Prober: &probing.ObservedGenerationProbe{
			Prober: readyConditionProbe,
		},
	}
	issuerProbe = &probing.GroupKindSelector{
		GroupKind: schema.GroupKind{Group: "acme.cert-manager.io", Kind: "Issuer"},
		Prober: &probing.ObservedGenerationProbe{
			Prober: readyConditionProbe,
		},
	}

	// deplStaefulSetProbe probes Deployment, StatefulSet objects.
	deplStatefulSetProbe = &probing.ObservedGenerationProbe{
		Prober: probing.And{
			availableConditionProbe,
			replicasUpdatedProbe,
		},
	}

	// Checks if the Type: "Available" Condition is "True".
	availableConditionProbe = &probing.ConditionProbe{ // "Available" == "True"
		Type:   string(appsv1.DeploymentAvailable),
		Status: string(corev1.ConditionTrue),
	}

	// Checks if the Type: "Ready" Condition is "True"
	readyConditionProbe = &probing.ObservedGenerationProbe{
		Prober: &probing.ConditionProbe{
			Type:   "Ready",
			Status: "True",
		},
	}

	// Checks if .status.updatedReplicas == .status.replicas.
	// Works for StatefulSts, Deployments and ReplicaSets.
	replicasUpdatedProbe = &probing.FieldsEqualProbe{
		FieldA: ".status.updatedReplicas",
		FieldB: ".status.replicas",
	}
)
