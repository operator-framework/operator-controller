package controllers

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	orbv1alpha1 "github.com/joelanford/orb-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/labels"
)

type OrbOperatorRevisionStatesGetter struct {
	Reader client.Reader
}

func (o *OrbOperatorRevisionStatesGetter) GetRevisionStates(ctx context.Context, ext *ocv1.ClusterExtension) (*RevisionStates, error) {
	// The orb ClusterObjectDeployment is named after the ClusterExtension, and
	// the orb controller stamps out ClusterObjectSet revisions whose spec.group
	// equals that name. Query them via the spec.group field indexer.
	existingRevisionList := &orbv1alpha1.ClusterObjectSetList{}
	if err := o.Reader.List(ctx, existingRevisionList, client.MatchingFields{
		"spec.group": ext.Name,
	}); err != nil {
		return nil, fmt.Errorf("listing revisions: %w", err)
	}
	slices.SortFunc(existingRevisionList.Items, func(a, b orbv1alpha1.ClusterObjectSet) int {
		return cmp.Compare(a.Spec.Revision, b.Spec.Revision)
	})

	// The ClusterObjectDeployment is named after the ClusterExtension. Its
	// Progressing condition carries the deployment-level rollout signal
	// (ProgressDeadlineExceeded, ReconcileError, ...) used to classify the active
	// revision. It may not exist yet on the first reconcile.
	var codProgressing *metav1.Condition
	cod := &orbv1alpha1.ClusterObjectDeployment{}
	if err := o.Reader.Get(ctx, client.ObjectKey{Name: ext.Name}, cod); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("getting ClusterObjectDeployment: %w", err)
		}
	} else {
		codProgressing = apimeta.FindStatusCondition(cod.Status.Conditions, orbv1alpha1.ConditionTypeProgressing)
	}

	rs := &RevisionStates{}
	for i := range existingRevisionList.Items {
		rev := &existingRevisionList.Items[i]
		if rev.Spec.LifecycleState == orbv1alpha1.LifecycleStateArchived {
			continue
		}

		// completedAt is set once (when all phases first become Available) and is
		// never cleared, so it is the signal that a revision is installed. The
		// current per-phase Available state is orthogonal and not consulted here.
		completed := rev.Status.CompletedAt != nil

		// The bundle metadata annotations are set by the applier on the COD
		// template metadata and propagated onto each revision by the orb
		// controller.
		rm := &RevisionMetadata{
			RevisionName: rev.Name,
			Package:      rev.Annotations[labels.PackageNameKey],
			Image:        rev.Annotations[labels.BundleReferenceKey],
			// Synthesize OLM-vocabulary Available/Progressing conditions so the
			// revision-state-driven apply step can mirror them onto the CE.
			Conditions: orbRevisionConditions(rev, codProgressing, completed),
			BundleMetadata: ocv1.BundleMetadata{
				Name:    rev.Annotations[labels.BundleNameKey],
				Version: rev.Annotations[labels.BundleVersionKey],
			},
		}
		// Only set Release if the annotation key exists (to distinguish "not set" from "explicitly empty")
		if releaseValue, ok := rev.Annotations[labels.BundleReleaseKey]; ok {
			rm.Release = &releaseValue
		}

		if completed {
			rs.Installed = rm
		} else {
			rs.RollingOut = append(rs.RollingOut, rm)
		}
	}

	return rs, nil
}

// orbErrorProgressingReasons are orb COD Progressing reasons that indicate a
// retryable rollout error; they map onto the ClusterExtension's Retrying reason.
var orbErrorProgressingReasons = map[string]struct{}{
	orbv1alpha1.ReasonReconcileError:  {},
	orbv1alpha1.ReasonInternalError:   {},
	orbv1alpha1.ReasonInvalidRevision: {},
	orbv1alpha1.ReasonTeardownError:   {},
}

// orbRevisionConditions synthesizes the ocv1 Available and Progressing conditions
// that drive ClusterExtension status for a single orb revision. The apply step
// (ApplyBundleWithRevisions) mirrors these onto the CE.
func orbRevisionConditions(rev *orbv1alpha1.ClusterObjectSet, codProgressing *metav1.Condition, completed bool) []metav1.Condition {
	conds := make([]metav1.Condition, 0, 2)

	// Available passes through unchanged: orb and OLM share the
	// "Available"/"Unavailable" condition type and reason strings.
	if avail := apimeta.FindStatusCondition(rev.Status.Conditions, orbv1alpha1.ConditionTypeAvailable); avail != nil {
		conds = append(conds, metav1.Condition{
			Type:    ocv1.ClusterObjectSetTypeAvailable,
			Status:  avail.Status,
			Reason:  avail.Reason,
			Message: avail.Message,
		})
	}

	conds = append(conds, orbProgressingCondition(rev, codProgressing, completed))
	return conds
}

// orbProgressingCondition maps orb's rollout state onto the CE Progressing
// condition, in priority order (see
// specs/2026-08-13-orb-operator-status-mapping):
//  1. COD ProgressDeadlineExceeded -> Progressing=False/ProgressDeadlineExceeded
//     (a status signal only; the controller keeps reconciling - not terminal).
//  2. A blocked phase (Invalid, or synced<total with objectDetails) -> Retrying.
//  3. COD error reason (ReconcileError/InternalError/InvalidRevision/
//     TeardownError) -> Retrying.
//  4. Otherwise -> RollingOut. A completed revision -> Succeeded.
func orbProgressingCondition(rev *orbv1alpha1.ClusterObjectSet, codProgressing *metav1.Condition, completed bool) metav1.Condition {
	cond := metav1.Condition{Type: ocv1.ClusterObjectSetTypeProgressing, Status: metav1.ConditionTrue}
	blockedMsg := orbBlockedPhaseMessage(rev)

	switch {
	case completed:
		cond.Reason = ocv1.ReasonSucceeded
		cond.Message = "Desired state reached"
	case codProgressing != nil && codProgressing.Reason == orbv1alpha1.ReasonProgressDeadlineExceeded:
		cond.Status = metav1.ConditionFalse
		cond.Reason = ocv1.ReasonProgressDeadlineExceeded
		cond.Message = codProgressing.Message
	case blockedMsg != "":
		cond.Reason = ocv1.ClusterObjectSetReasonRetrying
		cond.Message = blockedMsg
	case codProgressing != nil && isOrbErrorReason(codProgressing.Reason):
		cond.Reason = ocv1.ClusterObjectSetReasonRetrying
		cond.Message = codProgressing.Message
	default:
		cond.Reason = ocv1.ReasonRollingOut
		cond.Message = "Revision is rolling out"
		if codProgressing != nil && codProgressing.Message != "" {
			cond.Message = codProgressing.Message
		}
	}
	return cond
}

func isOrbErrorReason(reason string) bool {
	_, ok := orbErrorProgressingReasons[reason]
	return ok
}

// orbBlockedPhaseMessage returns a non-empty message when a phase of the revision
// reports an object that cannot be applied: a phase Status of Invalid (caught by
// orb's per-object preflight dry-run, e.g. an immutable-field collision), or a
// phase with synced<total and object-level failure details. This is the
// "legitimate problem" signal, distinct from WaitingForAssertions
// (synced==total, probes/assertions pending), which is a healthy rollout.
func orbBlockedPhaseMessage(rev *orbv1alpha1.ClusterObjectSet) string {
	for i := range rev.Status.ObservedPhases {
		phase := &rev.Status.ObservedPhases[i]
		blocked := phase.Status == orbv1alpha1.PhaseStatusInvalid ||
			(phase.ObjectCounts.Synced < phase.ObjectCounts.Total && len(phase.ObjectDetails) > 0)
		if !blocked {
			continue
		}
		if msg := firstObjectDetailMessage(phase); msg != "" {
			return msg
		}
		if phase.Message != "" {
			return phase.Message
		}
		return fmt.Sprintf("phase %q is not progressing", phase.Name)
	}
	return ""
}

func firstObjectDetailMessage(phase *orbv1alpha1.ObservedPhase) string {
	for i := range phase.ObjectDetails {
		od := &phase.ObjectDetails[i]
		if len(od.Messages) > 0 {
			return fmt.Sprintf("%s %s/%s: %s", od.Kind, od.Namespace, od.Name, od.Messages[0])
		}
	}
	return ""
}
