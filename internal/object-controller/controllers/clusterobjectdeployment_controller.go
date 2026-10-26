//go:build !standard

package controllers

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
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
	Clock  clock.Clock
}

//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterobjectdeployments,verbs=get;list;watch;update;patch;create;delete
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterobjectdeployments/status,verbs=update;patch
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterobjectdeployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=olm.operatorframework.io,resources=clusterobjectsets,verbs=get;list;watch;update;patch;create;delete

func (c *ClusterObjectDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("cluster-object-deployment")
	ctx = log.IntoContext(ctx, l)
	if c.Clock == nil {
		c.Clock = clock.RealClock{}
	}

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
	result, reconcileErr := c.reconcile(ctx, reconciled)

	if !equality.Semantic.DeepEqual(existing.Status, reconciled.Status) {
		if err := c.Client.Status().Update(ctx, reconciled); err != nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("updating status: %w", err))
		}
	}

	return result, reconcileErr
}

// reconcile garbage-collects excess archived revisions and reconciles the
// deployment's rollout (see reconcileRollout). Child events and the revision's
// fixed deadline drive subsequent reconciliations.
func (c *ClusterObjectDeploymentReconciler) reconcile(ctx context.Context, cod *ocv1.ClusterObjectDeployment) (ctrl.Result, error) {
	revisions, err := c.listRevisions(ctx, cod.Name)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Garbage collection is best-effort housekeeping and must not block rollout or
	// status reconciliation: a persistently failing prune (e.g. a delete denied by
	// an admission policy) would otherwise indefinitely stall deployment of an
	// updated template and leave status stale even with a healthy active revision.
	// Preserve the error, continue reconciling, and return it joined with the
	// rollout outcome so the failure is still surfaced and retried.
	gcErr := c.garbageCollectArchivedRevisions(ctx, cod, revisions)

	result, rolloutErr := c.reconcileRollout(ctx, cod, revisions)
	return result, errors.Join(gcErr, rolloutErr)
}

// reconcileRollout creates a new ClusterObjectSet revision when the template has
// changed (or no active revision exists) and reflects the latest revision's
// rollout progress in the deployment's Available and Progressing conditions.
func (c *ClusterObjectDeploymentReconciler) reconcileRollout(ctx context.Context, cod *ocv1.ClusterObjectDeployment, revisions []ocv1.ClusterObjectSet) (ctrl.Result, error) {
	hash := templateHash(cod.Spec.Template)

	setAvailable(cod, revisions)

	// The template metadata is propagated verbatim onto each revision's
	// ClusterObjectSet. Invalid labels or annotations would make that revision
	// rejected by the API server and stall the rollout with the real cause buried
	// in logs, so validate here and surface it on the Progressing condition instead.
	if msg := validateTemplateMetadata(cod.Spec.Template.Metadata); msg != "" {
		setProgressing(cod, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonInvalidSpecification, msg)
		return ctrl.Result{}, nil
	}

	latest := latestActiveRevision(revisions)
	if latest == nil || latest.Labels[labels.TemplateHashKey] != hash {
		var err error
		latest, err = c.createRevision(ctx, cod, revisions, hash)
		if err != nil {
			setProgressing(cod, metav1.ConditionTrue, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, "waiting for new revision to be created")
			return ctrl.Result{}, err
		}
	}

	// Completion is historical: later health changes cannot restart the deadline
	// or turn an already completed rollout into a failed rollout.
	if !latest.Status.CompletedAt.IsZero() {
		setProgressing(cod, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, "latest revision has progressed")
		return ctrl.Result{}, nil
	}

	remaining, hasDeadline := durationUntilDeploymentDeadline(c.Clock, cod, latest)
	if hasDeadline && remaining <= 0 {
		setProgressing(cod, metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded, "latest revision exceeded its progress deadline")
		return ctrl.Result{}, nil
	}

	setProgressing(cod, metav1.ConditionTrue, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, "latest revision is progressing")
	if hasDeadline {
		return ctrl.Result{RequeueAfter: remaining}, nil
	}
	return ctrl.Result{}, nil
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
func (c *ClusterObjectDeploymentReconciler) createRevision(ctx context.Context, cod *ocv1.ClusterObjectDeployment, revisions []ocv1.ClusterObjectSet, hash string) (*ocv1.ClusterObjectSet, error) {
	revision, err := nextRevisionNumber(revisions)
	if err != nil {
		return nil, fmt.Errorf("allocating revision for group %q: %w", cod.Name, err)
	}
	cos := buildClusterObjectSet(cod, revision, hash)
	if err := controllerutil.SetControllerReference(cod, cos, c.Scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference: %w", err)
	}
	if err := c.Client.Create(ctx, cos); err != nil {
		return nil, fmt.Errorf("creating revision %d: %w", revision, err)
	}
	return cos, nil
}

// durationUntilDeploymentDeadline excludes time before the revision has its
// server-assigned creation timestamp. The deployment supplies the budget, while
// the revision's completion latch ends it independently of current readiness.
func durationUntilDeploymentDeadline(clk clock.Clock, cod *ocv1.ClusterObjectDeployment, cos *ocv1.ClusterObjectSet) (time.Duration, bool) {
	if cos == nil || cos.CreationTimestamp.IsZero() || !cos.Status.CompletedAt.IsZero() ||
		cos.Spec.LifecycleState == ocv1.ClusterObjectSetLifecycleStateArchived || !cos.DeletionTimestamp.IsZero() {
		return 0, false
	}
	pd := cod.Spec.Template.Spec.ProgressDeadlineMinutes
	if pd <= 0 {
		return 0, false
	}
	deadline := cos.CreationTimestamp.Add(time.Duration(pd) * time.Minute)
	return deadline.Sub(clk.Now()), true
}

// newCODDeadlineAwareRateLimiter bounds error backoff as well as successful
// reconciles: controller-runtime ignores RequeueAfter when an error is returned.
func newCODDeadlineAwareRateLimiter(delegate workqueue.TypedRateLimiter[ctrl.Request], reader client.Reader, clk clock.Clock) *deadlineAwareRateLimiter {
	return &deadlineAwareRateLimiter{
		delegate: delegate,
		remaining: func(req ctrl.Request) (time.Duration, bool, error) {
			ctx := context.Background()
			cod := &ocv1.ClusterObjectDeployment{}
			if err := reader.Get(ctx, req.NamespacedName, cod); err != nil {
				return 0, false, client.IgnoreNotFound(err)
			}
			if !cod.DeletionTimestamp.IsZero() {
				return 0, false, nil
			}
			var revisions ocv1.ClusterObjectSetList
			if err := reader.List(ctx, &revisions, client.MatchingFields{clusterObjectSetGroupIndex: cod.Name}); err != nil {
				return 0, false, err
			}
			latest := latestActiveRevision(revisions.Items)
			if latest == nil || latest.Labels[labels.TemplateHashKey] != templateHash(cod.Spec.Template) {
				// Creating the desired revision is outside the MVP deadline.
				return 0, false, nil
			}
			remaining, hasDeadline := durationUntilDeploymentDeadline(clk, cod, latest)
			return remaining, hasDeadline, nil
		},
	}
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
// The message is capped (see maxConditionMessageBytes) so Status().Update is never
// rejected for an oversized message, which would drop the condition entirely.
func setCondition(cod *ocv1.ClusterObjectDeployment, condType string, status metav1.ConditionStatus, reason, message string) {
	apimeta.SetStatusCondition(&cod.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            capBytes(message, maxConditionMessageBytes),
		ObservedGeneration: cod.Generation,
	})
}

func (c *ClusterObjectDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if c.Clock == nil {
		c.Clock = clock.RealClock{}
	}
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			RateLimiter: newCODDeadlineAwareRateLimiter(
				workqueue.DefaultTypedControllerRateLimiter[ctrl.Request](),
				mgr.GetClient(), c.Clock,
			),
		}).
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

// maxConditionMessageBytes caps condition messages written by this controller. The API
// server rejects a condition message longer than 32768 bytes (metav1.Condition.message
// carries +kubebuilder:validation:MaxLength=32768), which would drop the condition
// entirely; 1024 bytes is ample for a human-readable diagnostic and stays well clear of
// that limit.
const maxConditionMessageBytes = 1024

// maxBadValueBytes is the largest rejected label/annotation value echoed in a
// diagnostic. Longer values are omitted so a single value cannot bloat the message.
const maxBadValueBytes = 64

// validateTemplateMetadata validates the labels and annotations the deployment
// would propagate to each revision, using the same rules the API server applies
// to object metadata (key/value format for labels, key format and total size for
// annotations). It returns a user-facing message describing the problem, or an
// empty string if the metadata is valid.
func validateTemplateMetadata(md ocv1.ClusterObjectDeploymentTemplateMetadata) string {
	base := field.NewPath("spec", "template", "metadata")
	var errs field.ErrorList
	errs = append(errs, metav1validation.ValidateLabels(md.Labels, base.Child("labels"))...)
	errs = append(errs, apivalidation.ValidateAnnotations(md.Annotations, base.Child("annotations"))...)
	if len(errs) == 0 {
		return ""
	}
	// Field errors echo the rejected value, and a label value is not length-bounded by
	// the template schema. Omit oversized values rather than echoing them; the aggregated
	// message is additionally capped when written to a condition (see setCondition).
	for _, err := range errs {
		if v, ok := err.BadValue.(string); ok && len(v) > maxBadValueBytes {
			err.BadValue = fmt.Sprintf("<%d bytes omitted>", len(v))
		}
	}
	return fmt.Sprintf("template metadata is invalid: %v", errs.ToAggregate())
}

// capBytes truncates s to at most maxBytes bytes, appending a marker when it does so.
func capBytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	const marker = "...[truncated]"
	return truncateBytes(s, maxBytes-len(marker)) + marker
}

// truncateBytes returns the longest prefix of s that is at most maxBytes long without
// splitting a multi-byte UTF-8 rune.
func truncateBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	b := s[:maxBytes]
	for len(b) > 0 && !utf8.ValidString(b) {
		b = b[:len(b)-1]
	}
	return b
}

// buildClusterObjectSet constructs the ClusterObjectSet for a revision from the
// deployment template. It does not set the owner reference; the caller does.
func buildClusterObjectSet(cod *ocv1.ClusterObjectDeployment, revision int64, hash string) *ocv1.ClusterObjectSet {
	// Propagate the template's metadata labels and annotations verbatim to the
	// revision. The controller-managed TemplateHashKey label is set last so it
	// always wins over any caller-supplied value; it records which template produced
	// this revision so the controller can detect template changes. Ownership is
	// expressed through the controller owner reference and the revision's spec.group,
	// so no owner-kind/owner-name labels are set.
	revLabels := maps.Clone(cod.Spec.Template.Metadata.Labels)
	if revLabels == nil {
		revLabels = map[string]string{}
	}
	revLabels[labels.TemplateHashKey] = hash

	return &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-%d", cod.Name, revision),
			Labels:      revLabels,
			Annotations: maps.Clone(cod.Spec.Template.Metadata.Annotations),
		},
		Spec: ocv1.ClusterObjectSetSpec{
			Group:               cod.Name,
			Revision:            revision,
			LifecycleState:      ocv1.ClusterObjectSetLifecycleStateActive,
			Phases:              cod.Spec.Template.Spec.Phases,
			ProgressionProbes:   cod.Spec.Template.Spec.ProgressionProbes,
			CollisionProtection: cod.Spec.Template.Spec.CollisionProtection,
		},
	}
}
