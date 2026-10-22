//go:build !standard

package controllers

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	hashutil "github.com/operator-framework/operator-controller/internal/shared/util/hash"
)

// defaultRevisionHistoryLimit is the number of archived revisions retained when
// spec.revisionHistoryLimit is unset. It mirrors the CRD default and is applied
// defensively for callers (e.g. tests) that bypass API-server defaulting.
const defaultRevisionHistoryLimit = 5

// ClusterObjectDeploymentReconciler reconciles ClusterObjectDeployment objects by
// managing their owned ClusterObjectSet revisions.
type ClusterObjectDeploymentReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterobjectdeployments,verbs=get;list;watch;update;patch;create;delete
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterobjectdeployments/status,verbs=update;patch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterobjectdeployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterobjectsets,verbs=get;list;watch;update;patch;create;delete

func (c *ClusterObjectDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("cluster-object-deployment")
	ctx = log.IntoContext(ctx, l)

	existing := &ocv1.ClusterObjectDeployment{}
	if err := c.Client.Get(ctx, req.NamespacedName, existing); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !existing.DeletionTimestamp.IsZero() {
		// Owned ClusterObjectSets are removed by Kubernetes cascading deletion via
		// their controller owner reference; nothing to do here.
		return ctrl.Result{}, nil
	}

	l.Info("reconcile starting")
	defer l.Info("reconcile ending")

	reconciled := existing.DeepCopy()
	reconcileErr := c.reconcile(ctx, reconciled)

	if !equality.Semantic.DeepEqual(existing.Status, reconciled.Status) {
		if err := c.Client.Status().Update(ctx, reconciled); err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("updating status: %w", err))
		}
	}

	return ctrl.Result{}, reconcileErr
}

// reconcile garbage-collects excess archived revisions and reconciles the
// deployment's rollout (see reconcileRollout). Requeues are driven by the
// controller's watch on owned ClusterObjectSets.
func (c *ClusterObjectDeploymentReconciler) reconcile(ctx context.Context, cod *ocv1.ClusterObjectDeployment) error {
	revisions, err := c.listRevisions(ctx, cod.Name)
	if err != nil {
		return err
	}

	// Garbage collection is best-effort housekeeping and must not block rollout or
	// status reconciliation: a persistently failing prune (e.g. a delete denied by
	// an admission policy) would otherwise indefinitely stall deployment of an
	// updated template and leave status stale even with a healthy active revision.
	// Preserve the error, continue reconciling, and return it joined with the
	// rollout outcome so the failure is still surfaced and retried.
	gcErr := c.garbageCollectArchivedRevisions(ctx, cod, revisions)

	return errors.Join(gcErr, c.reconcileRollout(ctx, cod, revisions))
}

// reconcileRollout creates a new ClusterObjectSet revision when the template has
// changed (or no active revision exists) and reflects the latest revision's
// rollout progress in the deployment's Available and Progressing conditions.
func (c *ClusterObjectDeploymentReconciler) reconcileRollout(ctx context.Context, cod *ocv1.ClusterObjectDeployment, revisions []ocv1.ClusterObjectSet) error {
	hash := templateHash(cod.Spec.Template)

	setAvailable(cod, revisions)

	latest := latestActiveRevision(revisions)
	if latest == nil || latest.Labels[labels.TemplateHashKey] != hash {
		if err := c.createRevision(ctx, cod, revisions, hash); err != nil {
			setProgressing(cod, metav1.ConditionTrue, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, "waiting for new revision to be created")
			return err
		}
		setProgressing(cod, metav1.ConditionTrue, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, "waiting for new revision to be created")
		return nil
	}

	// A revision that has exceeded its progress deadline reports Ready=False with
	// reason ProgressDeadlineExceeded and never records completedAt. Surface that
	// terminal rollout state on the deployment's Progressing condition so it is
	// actionable, rather than reporting an indefinitely-progressing rollout.
	if ready := apimeta.FindStatusCondition(latest.Status.Conditions, ocv1.ClusterObjectSetTypeReady); ready != nil &&
		ready.Status == metav1.ConditionFalse && ready.Reason == ocv1.ReasonProgressDeadlineExceeded {
		setProgressing(cod, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded, "latest revision exceeded its progress deadline")
		return nil
	}

	if latest.Status.CompletedAt.IsZero() {
		setProgressing(cod, metav1.ConditionTrue, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, "latest revision is progressing")
	} else {
		setProgressing(cod, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, "latest revision has progressed")
	}
	return nil
}

// listRevisions returns all ClusterObjectSets in the deployment's group, sorted
// ascending by revision number.
func (c *ClusterObjectDeploymentReconciler) listRevisions(ctx context.Context, group string) ([]ocv1.ClusterObjectSet, error) {
	var list ocv1.ClusterObjectSetList
	if err := c.Client.List(ctx, &list, client.MatchingFields{clusterObjectSetGroupIndex: group}); err != nil {
		return nil, fmt.Errorf("listing revisions for group %q: %w", group, err)
	}
	slices.SortFunc(list.Items, func(a, b ocv1.ClusterObjectSet) int {
		return cmp.Compare(a.Spec.Revision, b.Spec.Revision)
	})
	return list.Items, nil
}

// garbageCollectArchivedRevisions deletes the oldest archived ClusterObjectSet
// revisions in excess of spec.revisionHistoryLimit. Active revisions are never
// deleted and do not count against the limit. A nil limit falls back to
// defaultRevisionHistoryLimit; a limit of 0 retains no archived revisions.
//
// Only archived revisions that have finished tearing down are candidates for
// pruning (see archivedTeardownComplete). Deleting a revision mid-teardown would
// send it down the ClusterObjectSet controller's deletion path, which removes the
// teardown finalizer without calling RevisionEngine.Teardown and so bypasses
// ordered reverse-phase cleanup (e.g. a controller could be removed before the
// resources it finalizes have terminated). A revision still tearing down is
// skipped and pruned on a later reconcile, which the controller's watch on owned
// ClusterObjectSets triggers once teardown completes.
//
// revisions must be sorted ascending by revision number (oldest first), as
// returned by listRevisions.
//
// NOTE: The candidate revisions come from listRevisions, which selects by
// spec.group (equal to the deployment name) rather than by this deployment's
// controller owner reference. This is deliberate and consistent with the rest of
// OLMv1: sibling membership of a revision family is defined by spec.group, not by
// ownership (see createRevision's naming/grouping and the ClusterObjectSet
// controller's own sibling archival, both keyed on spec.group). A consequence is
// that a ClusterObjectSet sharing this group but created by a different owner
// would be counted toward the limit and could be deleted here. That is tolerated
// because the deployment name is the group and is treated as an exclusive
// namespace for its revisions — nothing else should create ClusterObjectSets in
// it. If siblinghood is ever redefined in terms of ownership, this function and
// listRevisions must be updated together to filter by owner reference and UID.
func (c *ClusterObjectDeploymentReconciler) garbageCollectArchivedRevisions(ctx context.Context, cod *ocv1.ClusterObjectDeployment, revisions []ocv1.ClusterObjectSet) error {
	limit := defaultRevisionHistoryLimit
	if cod.Spec.RevisionHistoryLimit != nil {
		limit = int(*cod.Spec.RevisionHistoryLimit)
	}

	archived := make([]*ocv1.ClusterObjectSet, 0, len(revisions))
	for i := range revisions {
		rev := &revisions[i]
		if rev.DeletionTimestamp.IsZero() &&
			rev.Spec.LifecycleState == ocv1.ClusterObjectSetLifecycleStateArchived &&
			archivedTeardownComplete(rev) {
			archived = append(archived, rev)
		}
	}

	// Delete the oldest archived revisions beyond the limit. Attempt every
	// excess revision even if one fails, so a single revision that cannot be
	// deleted (e.g. a denying webhook or a conflict) does not block pruning of
	// the others on every reconcile.
	var errs []error
	for i := 0; i < len(archived)-limit; i++ {
		if err := c.Client.Delete(ctx, archived[i]); err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("deleting archived revision %q: %w", archived[i].Name, err))
		}
	}
	return errors.Join(errs...)
}

// archivedTeardownComplete reports whether an archived revision has finished
// tearing down its managed objects, as signalled by its Ready condition
// (Ready=False/Archived). The ClusterObjectSet controller sets this reason only
// after RevisionEngine.Teardown completes; while teardown is still in progress it
// reports Ready=False/Archiving.
func archivedTeardownComplete(rev *ocv1.ClusterObjectSet) bool {
	cond := apimeta.FindStatusCondition(rev.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
	return cond != nil && cond.Reason == ocv1.ClusterObjectSetReasonArchived
}

// createRevision builds and creates the next ClusterObjectSet revision owned by
// the deployment. Any create error, including AlreadyExists, is returned.
//
// TODO: revisit AlreadyExists handling under the adoption use-case.
func (c *ClusterObjectDeploymentReconciler) createRevision(ctx context.Context, cod *ocv1.ClusterObjectDeployment, revisions []ocv1.ClusterObjectSet, hash string) error {
	revision, err := nextRevisionNumber(revisions)
	if err != nil {
		return fmt.Errorf("allocating revision for group %q: %w", cod.Name, err)
	}
	cos := buildClusterObjectSet(cod, revision, hash)
	if err := controllerutil.SetControllerReference(cod, cos, c.Scheme); err != nil {
		return fmt.Errorf("setting controller reference: %w", err)
	}
	if err := c.Client.Create(ctx, cos); err != nil {
		return fmt.Errorf("creating revision %d: %w", revision, err)
	}
	return nil
}

// setAvailable sets the deployment's Available condition. It is True when at least
// one active (non-archived, non-deleting) revision is Ready, so the deployment
// stays available across rollouts while a prior revision remains Ready.
func setAvailable(cod *ocv1.ClusterObjectDeployment, revisions []ocv1.ClusterObjectSet) {
	activeCount := 0
	for i := range revisions {
		rev := &revisions[i]
		if !rev.DeletionTimestamp.IsZero() || rev.Spec.LifecycleState == ocv1.ClusterObjectSetLifecycleStateArchived {
			continue
		}
		activeCount++
		if apimeta.IsStatusConditionTrue(rev.Status.Conditions, ocv1.ClusterObjectSetTypeReady) {
			setCondition(cod, ocv1.ClusterObjectDeploymentTypeAvailable, metav1.ConditionTrue,
				ocv1.ClusterObjectDeploymentReasonAvailable, "an active revision is ready")
			return
		}
	}
	if activeCount == 0 {
		setCondition(cod, ocv1.ClusterObjectDeploymentTypeAvailable, metav1.ConditionFalse,
			ocv1.ClusterObjectDeploymentReasonNoActiveRevision, "no active revision exists")
		return
	}
	setCondition(cod, ocv1.ClusterObjectDeploymentTypeAvailable, metav1.ConditionFalse,
		ocv1.ClusterObjectDeploymentReasonUnavailable, "no active revision is ready")
}

// setProgressing sets the deployment's Progressing condition.
func setProgressing(cod *ocv1.ClusterObjectDeployment, status metav1.ConditionStatus, reason, message string) {
	setCondition(cod, ocv1.ClusterObjectDeploymentTypeProgressing, status, reason, message)
}

// setCondition sets a condition on the deployment status with the observed generation.
func setCondition(cod *ocv1.ClusterObjectDeployment, condType string, status metav1.ConditionStatus, reason, message string) {
	apimeta.SetStatusCondition(&cod.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cod.Generation,
	})
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

// templateHash returns a stable digest of the deployment template. Any change to
// the template (including changes to fields that are mutable on a ClusterObjectSet)
// produces a different digest, which is how the controller decides a new revision
// is required. The shared DeepHashObject helper base36-encodes a sha224 of the
// template's JSON, which is label-safe and short enough to use as a label value
// without truncation.
func templateHash(template ocv1.ClusterObjectDeploymentTemplate) string {
	return hashutil.DeepHashObject(template)
}

// nextRevisionNumber returns the revision number for a new revision: one greater
// than the highest revision across all revisions in the group (including archived
// ones, so revision numbers are never reused). It returns an error at the limit.
func nextRevisionNumber(revisions []ocv1.ClusterObjectSet) (int64, error) {
	var max int64
	for i := range revisions {
		if revisions[i].Spec.Revision > max {
			max = revisions[i].Spec.Revision
		}
	}
	if max >= ocv1.ClusterObjectSetMaxRevision {
		return 0, fmt.Errorf("revision limit %d reached", ocv1.ClusterObjectSetMaxRevision)
	}
	return max + 1, nil
}

// latestActiveRevision returns the highest-revision ClusterObjectSet that is
// neither archived nor being deleted, or nil if there is none.
func latestActiveRevision(revisions []ocv1.ClusterObjectSet) *ocv1.ClusterObjectSet {
	var latest *ocv1.ClusterObjectSet
	for i := range revisions {
		rev := &revisions[i]
		if !rev.DeletionTimestamp.IsZero() || rev.Spec.LifecycleState == ocv1.ClusterObjectSetLifecycleStateArchived {
			continue
		}
		if latest == nil || rev.Spec.Revision > latest.Spec.Revision {
			latest = rev
		}
	}
	return latest
}

// buildClusterObjectSet constructs the ClusterObjectSet for a revision from the
// deployment template. It does not set the owner reference; the caller does.
func buildClusterObjectSet(cod *ocv1.ClusterObjectDeployment, revision int64, hash string) *ocv1.ClusterObjectSet {
	return &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%d", cod.Name, revision),
			Labels: map[string]string{
				// TemplateHashKey records which template produced this revision so the
				// controller can detect template changes. Ownership is expressed through
				// the controller owner reference and the revision's spec.group, so no
				// owner-kind/owner-name labels are set.
				labels.TemplateHashKey: hash,
			},
		},
		Spec: ocv1.ClusterObjectSetSpec{
			Group:                   cod.Name,
			Revision:                revision,
			LifecycleState:          ocv1.ClusterObjectSetLifecycleStateActive,
			Phases:                  cod.Spec.Template.Spec.Phases,
			ProgressDeadlineMinutes: cod.Spec.Template.Spec.ProgressDeadlineMinutes,
			ProgressionProbes:       cod.Spec.Template.Spec.ProgressionProbes,
			CollisionProtection:     cod.Spec.Template.Spec.CollisionProtection,
		},
	}
}
