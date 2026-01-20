//go:build !standard

package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	"sigs.k8s.io/controller-runtime/pkg/event"
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
	Client                client.Client
	RevisionEngineFactory RevisionEngineFactory
	TrackingCache         trackingCache
	// track if we have queued up the reconciliation that detects eventual progress deadline issues
	// keys is revision UUID, value is boolean
	progressDeadlineCheckInFlight sync.Map
}

type trackingCache interface {
	client.Reader
	Source(handler handler.EventHandler, predicates ...predicate.Predicate) source.Source
	Watch(ctx context.Context, user client.Object, gvks sets.Set[schema.GroupVersionKind]) error
	Free(ctx context.Context, user client.Object) error
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

	if pd := existingRev.Spec.ProgressDeadlineMinutes; pd > 0 {
		cnd := meta.FindStatusCondition(reconciledRev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing)
		isStillProgressing := cnd != nil && cnd.Status == metav1.ConditionTrue && cnd.Reason != ocv1.ReasonSucceeded
		succeeded := meta.IsStatusConditionTrue(reconciledRev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeSucceeded)
		// check if we reached the progress deadline only if the revision is still progressing and has not succeeded yet
		if isStillProgressing && !succeeded {
			timeout := time.Duration(pd) * time.Minute
			if time.Since(existingRev.CreationTimestamp.Time) > timeout {
				// progress deadline reached, reset any errors and stop reconciling this revision
				markAsNotProgressing(reconciledRev, ocv1.ReasonProgressDeadlineExceeded, fmt.Sprintf("Revision has not rolled out for %d minutes.", pd))
				reconcileErr = nil
				res = ctrl.Result{}
			} else if _, found := c.progressDeadlineCheckInFlight.Load(existingRev.GetUID()); !found && reconcileErr == nil {
				// if we haven't already queued up a reconcile to check for progress deadline, queue one up, but only once
				c.progressDeadlineCheckInFlight.Store(existingRev.GetUID(), true)
				res = ctrl.Result{RequeueAfter: timeout}
			}
		}
	}
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
		setRetryingConditions(rev, err.Error())
		return ctrl.Result{}, fmt.Errorf("converting to boxcutter revision: %v", err)
	}

	if !rev.DeletionTimestamp.IsZero() || rev.Spec.LifecycleState == ocv1.ClusterExtensionRevisionLifecycleStateArchived {
		return c.teardown(ctx, rev)
	}

	revVersion := rev.GetAnnotations()[labels.BundleVersionKey]
	//
	// Reconcile
	//
	if err := c.ensureFinalizer(ctx, rev, clusterExtensionRevisionTeardownFinalizer); err != nil {
		return ctrl.Result{}, fmt.Errorf("error ensuring teardown finalizer: %v", err)
	}

	if err := c.establishWatch(ctx, rev, revision); err != nil {
		werr := fmt.Errorf("establish watch: %v", err)
		setRetryingConditions(rev, werr.Error())
		return ctrl.Result{}, werr
	}

	revisionEngine, err := c.RevisionEngineFactory.CreateRevisionEngine(ctx, rev)
	if err != nil {
		setRetryingConditions(rev, err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to create revision engine: %v", err)
	}

	rres, err := revisionEngine.Reconcile(ctx, *revision, opts...)
	if err != nil {
		if rres != nil {
			// Log detailed reconcile reports only in debug mode (V(1)) to reduce verbosity.
			l.V(1).Info("reconcile report", "report", rres.String())
		}
		setRetryingConditions(rev, err.Error())
		return ctrl.Result{}, fmt.Errorf("revision reconcile: %v", err)
	}

	// Retry failing preflight checks with a flat 10s retry.
	// TODO: report status, backoff?
	if verr := rres.GetValidationError(); verr != nil {
		l.Error(fmt.Errorf("%w", verr), "preflight validation failed, retrying after 10s")
		setRetryingConditions(rev, fmt.Sprintf("revision validation error: %s", verr))
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	for i, pres := range rres.GetPhases() {
		if verr := pres.GetValidationError(); verr != nil {
			l.Error(fmt.Errorf("%w", verr), "phase preflight validation failed, retrying after 10s", "phase", i)
			setRetryingConditions(rev, fmt.Sprintf("phase %d validation error: %s", i, verr))
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
			setRetryingConditions(rev, fmt.Sprintf("revision object collisions in phase %d\n%s", i, strings.Join(collidingObjs, "\n\n")))
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	if !rres.InTransition() {
		markAsProgressing(rev, ocv1.ReasonSucceeded, fmt.Sprintf("Revision %s has rolled out.", revVersion))
	} else {
		markAsProgressing(rev, ocv1.ReasonRollingOut, fmt.Sprintf("Revision %s is rolling out.", revVersion))
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

		markAsAvailable(rev, ocv1.ClusterExtensionRevisionReasonProbesSucceeded, "Objects are available and pass all probes.")

		// We'll probably only want to remove this once we are done updating the ClusterExtension conditions
		// as its one of the interfaces between the revision and the extension. If we still have the Succeeded for now
		// that's fine.
		meta.SetStatusCondition(&rev.Status.Conditions, metav1.Condition{
			Type:               ocv1.ClusterExtensionRevisionTypeSucceeded,
			Status:             metav1.ConditionTrue,
			Reason:             ocv1.ReasonSucceeded,
			Message:            "Revision succeeded rolling out.",
			ObservedGeneration: rev.Generation,
		})
	} else {
		var probeFailureMsgs []string
		for _, pres := range rres.GetPhases() {
			if pres.IsComplete() {
				continue
			}
			for _, ores := range pres.GetObjects() {
				// we probably want an AvailabilityProbeType and run through all of them independently of whether
				// the revision is complete or not
				pr := ores.ProbeResults()[boxcutter.ProgressProbeType]
				if pr.Status == machinerytypes.ProbeStatusTrue {
					continue
				}

				obj := ores.Object()
				gvk := obj.GetObjectKind().GroupVersionKind()
				// I think these can be pretty large and verbose. We may want to
				// work a little on the formatting...?
				probeFailureMsgs = append(probeFailureMsgs, fmt.Sprintf(
					"Object %s.%s %s/%s: %v",
					gvk.Kind, gvk.GroupVersion().String(),
					obj.GetNamespace(), obj.GetName(), strings.Join(pr.Messages, " and "),
				))
				break
			}
		}

		if len(probeFailureMsgs) > 0 {
			markAsUnavailable(rev, ocv1.ClusterExtensionRevisionReasonProbeFailure, strings.Join(probeFailureMsgs, "\n"))
		} else {
			markAsUnavailable(rev, ocv1.ReasonRollingOut, fmt.Sprintf("Revision %s is rolling out.", revVersion))
		}
	}

	return ctrl.Result{}, nil
}

func (c *ClusterExtensionRevisionReconciler) teardown(ctx context.Context, rev *ocv1.ClusterExtensionRevision) (ctrl.Result, error) {
	if err := c.TrackingCache.Free(ctx, rev); err != nil {
		markAsAvailableUnknown(rev, ocv1.ClusterExtensionRevisionReasonReconciling, err.Error())
		return ctrl.Result{}, fmt.Errorf("error stopping informers: %v", err)
	}

	// Ensure conditions are set before removing the finalizer when archiving
	if rev.Spec.LifecycleState == ocv1.ClusterExtensionRevisionLifecycleStateArchived && markAsArchived(rev) {
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
	skipProgressDeadlineExceededPredicate := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			rev, ok := e.ObjectNew.(*ocv1.ClusterExtensionRevision)
			if !ok {
				return true
			}
			// allow deletions to happen
			if !rev.DeletionTimestamp.IsZero() {
				return true
			}
			if cnd := meta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterExtensionRevisionTypeProgressing); cnd != nil && cnd.Status == metav1.ConditionFalse && cnd.Reason == ocv1.ReasonProgressDeadlineExceeded {
				return false
			}
			return true
		},
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(
			&ocv1.ClusterExtensionRevision{},
			builder.WithPredicates(
				predicate.ResourceVersionChangedPredicate{},
				skipProgressDeadlineExceededPredicate,
			),
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

func setRetryingConditions(cer *ocv1.ClusterExtensionRevision, message string) {
	markAsProgressing(cer, ocv1.ClusterExtensionRevisionReasonRetrying, message)
	if meta.FindStatusCondition(cer.Status.Conditions, ocv1.ClusterExtensionRevisionTypeAvailable) != nil {
		markAsAvailableUnknown(cer, ocv1.ClusterExtensionRevisionReasonReconciling, message)
	}
}

func markAsProgressing(cer *ocv1.ClusterExtensionRevision, reason, message string) {
	meta.SetStatusCondition(&cer.Status.Conditions, metav1.Condition{
		Type:               ocv1.ClusterExtensionRevisionTypeProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cer.Generation,
	})
}

func markAsNotProgressing(cer *ocv1.ClusterExtensionRevision, reason, message string) bool {
	return meta.SetStatusCondition(&cer.Status.Conditions, metav1.Condition{
		Type:               ocv1.ClusterExtensionRevisionTypeProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cer.Generation,
	})
}

func markAsAvailable(cer *ocv1.ClusterExtensionRevision, reason, message string) bool {
	return meta.SetStatusCondition(&cer.Status.Conditions, metav1.Condition{
		Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cer.Generation,
	})
}

func markAsUnavailable(cer *ocv1.ClusterExtensionRevision, reason, message string) {
	meta.SetStatusCondition(&cer.Status.Conditions, metav1.Condition{
		Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cer.Generation,
	})
}

func markAsAvailableUnknown(cer *ocv1.ClusterExtensionRevision, reason, message string) bool {
	return meta.SetStatusCondition(&cer.Status.Conditions, metav1.Condition{
		Type:               ocv1.ClusterExtensionRevisionTypeAvailable,
		Status:             metav1.ConditionUnknown,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cer.Generation,
	})
}

func markAsArchived(cer *ocv1.ClusterExtensionRevision) bool {
	const msg = "revision is archived"
	updated := markAsNotProgressing(cer, ocv1.ClusterExtensionRevisionReasonArchived, msg)
	return markAsAvailableUnknown(cer, ocv1.ClusterExtensionRevisionReasonArchived, msg) || updated
}
