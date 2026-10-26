package status

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/validation"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// sumObjectCounts aggregates ObjectCounts across all observed phases into a single total.
// Returns ObjectCounts with a Total of zero if phases is empty.
func sumObjectCounts(phases []ocv1.ObservedPhase) ocv1.ObjectCounts {
	counts := ocv1.ObjectCounts{
		Total: 0,
	}
	if len(phases) == 0 {
		return counts
	}
	for i := range phases {
		counts.Total += phases[i].ObjectCounts.Total
		counts.Present += phases[i].ObjectCounts.Present
		counts.Synced += phases[i].ObjectCounts.Synced
		counts.Ready += phases[i].ObjectCounts.Ready
	}
	return counts
}

func UpdateObservedPhasesForReconcile(objSet *ocv1.ClusterObjectSet, rres machinery.RevisionResult, now metav1.Time) {
	if objSet == nil || rres == nil {
		return
	}
	if rres.GetValidationError() != nil {
		return
	}

	// Calculate totals from the spec
	phaseTotals := make(map[string]int64)
	for _, phase := range objSet.Spec.Phases {
		phaseTotals[phase.Name] = int64(len(phase.Objects))
	}

	// Phases are reconciled in order; once a phase is incomplete, later phases are
	// only observed read-only (paused). A paused phase's preflight dry-run can fail
	// for reasons that resolve once earlier phases roll out (e.g. a Namespace a
	// later phase depends on has not been created yet), so its validation error is
	// not actionable yet. Only treat a phase's validation error as a terminal
	// validation failure while every preceding phase is complete; otherwise skip it
	// and let the rollout status (and the progress deadline) below take over.
	// Collisions are still reported for paused phases below: an object owned by
	// another controller does not become ownable just because earlier phases roll
	// out, so it remains actionable regardless of phase ordering.
	precedingPhasesComplete := true
	for _, pres := range rres.GetPhases() {
		observedPhase := ocv1.ObservedPhase{
			Name: pres.GetName(),
		}
		observedPhase.ObjectCounts.Total = phaseTotals[pres.GetName()]

		if pres.GetValidationError() != nil {
			if !precedingPhasesComplete {
				observedPhase.Status = ocv1.PhaseStatusPending
				observedPhase.Message = "waiting for earlier phases to complete"
			} else if isRecoverablePhaseValidation(pres.GetValidationError()) {
				observedPhase.Status = ocv1.PhaseStatusPreconditionsNotMet
				observedPhase.Message = truncateMessage(pres.GetValidationError().Error())
			} else {
				observedPhase.Status = ocv1.PhaseStatusInvalidSpecification
				observedPhase.Message = truncateMessage(pres.GetValidationError().Error())
			}
			// A validation-error phase is not complete; record that for subsequent phases.
			precedingPhasesComplete = false
			updateObservedPhaseStatus(objSet, observedPhase, now)
			continue
		}

		if !pres.IsComplete() {
			precedingPhasesComplete = false
		}

		isPaused := false
		for _, pobj := range pres.GetObjects() {
			if pobj.IsComplete() {
				observedPhase.ObjectCounts.Ready++
			}
			switch pobj.Action() {
			case machinery.ActionCreated:
				if !pobj.IsPaused() {
					observedPhase.ObjectCounts.Present++
					observedPhase.ObjectCounts.Synced++
				}
			case machinery.ActionIdle:
				observedPhase.ObjectCounts.Present++
				observedPhase.ObjectCounts.Synced++
			case machinery.ActionProgressed:
				observedPhase.ObjectCounts.Present++
			case machinery.ActionUpdated, machinery.ActionRecovered:
				observedPhase.ObjectCounts.Present++
				if !pobj.IsPaused() {
					observedPhase.ObjectCounts.Synced++
				}
			case machinery.ActionCollision:
				// TODO: collect object collisions and put in status
			}

			// track for phase status derivation
			if pobj.IsPaused() {
				isPaused = true
			}
		}

		var (
			// TODO: consider reframing as one or more objects have transitioned (rather than all - since there can be differences b/w the revisions)
			isSuperseded  = (HasSupersededObject(pres) && pres.IsComplete()) || (len(pres.GetObjects()) > 0 && pres.HasProgressed())
			isReady       = pres.IsComplete() && !isSuperseded
			isSynced      = observedPhase.ObjectCounts.Synced == observedPhase.ObjectCounts.Total
			isReconciling = !isSynced && !isPaused
		)

		switch {
		case isReady:
			observedPhase.Status = ocv1.PhaseStatusReady
			observedPhase.Message = "all objects are ready"
		case isSuperseded:
			observedPhase.Status = ocv1.PhaseStatusSuperseded
			observedPhase.Message = "all objects have progressed to a new revision"
		case isSynced:
			observedPhase.Status = ocv1.PhaseStatusStabilizing
			observedPhase.Message = "all objects are synced; waiting for them to become ready"
		case isReconciling:
			// TODO: deal with surfacing collisions
			// TODO: reconsider splitting into the cases where reconciling might appear (e.g. ObjectCollision, objects are transitioning to another revision, etc.)
			observedPhase.Status = ocv1.PhaseStatusReconciling
			observedPhase.Message = "phase is being reconciled"
		default:
			// read-only while an earlier phase is still rolling out.
			observedPhase.Status = ocv1.PhaseStatusPending
			observedPhase.Message = "waiting for previous phases to be ready"
		}

		if !isReady && !isSuperseded {
			for _, obj := range pres.GetObjects() {
				if obj.IsComplete() || obj.Action() == machinery.ActionProgressed {
					continue
				}
				observedPhase.ObjectDetails = append(observedPhase.ObjectDetails, objectStatus(obj))
			}
		}
		updateObservedPhaseStatus(objSet, observedPhase, now)
	}
	objSet.Status.ObjectCounts = sumObjectCounts(objSet.Status.ObservedPhases)
}

func UpdateObservedPhasesForTeardown(objSet *ocv1.ClusterObjectSet, tres machinery.RevisionTeardownResult, now metav1.Time) {
	if objSet == nil || tres == nil {
		return
	}

	// Calculate totals from the spec
	phaseTotals := make(map[string]int64)
	for _, phase := range objSet.Spec.Phases {
		phaseTotals[phase.Name] = int64(len(phase.Objects))
	}

	for _, pres := range tres.GetPhases() {
		observedPhase := ocv1.ObservedPhase{
			Name: pres.GetName(),
		}

		observedPhase.ObjectCounts.Total = phaseTotals[pres.GetName()]
		observedPhase.ObjectCounts.Present = int64(len(pres.Waiting()))
		observedPhase.ObjectCounts.Synced = 0

		if name, ok := tres.GetActivePhaseName(); ok && name == pres.GetName() {
			observedPhase.Status = ocv1.PhaseStatusArchiving
			observedPhase.Message = "removing all phase objects owned by this object set"
		} else if slices.Contains(tres.GetGonePhaseNames(), pres.GetName()) {
			observedPhase.Status = ocv1.PhaseStatusArchived
			observedPhase.Message = "all phase objects owned by this object set have been removed"
		} else {
			observedPhase.Status = ocv1.PhaseStatusPending
			observedPhase.Message = "waiting for later phases to complete teardown"
		}
		for _, ref := range pres.Waiting() {
			observedPhase.ObjectDetails = append(observedPhase.ObjectDetails, ocv1.ObjectStatus{
				Group:     ref.Group,
				Version:   ref.Version,
				Kind:      ref.Kind,
				Namespace: ref.Namespace,
				Name:      ref.Name,
				Messages:  []string{"awaiting deletion"},
			})
		}
		updateObservedPhaseStatus(objSet, observedPhase, now)
	}
	objSet.Status.ObjectCounts = sumObjectCounts(objSet.Status.ObservedPhases)
}

func updateObservedPhaseStatus(objSet *ocv1.ClusterObjectSet, observedPhase ocv1.ObservedPhase, now metav1.Time) {
	if objSet == nil {
		return
	}

	for i, ophase := range objSet.Status.ObservedPhases {
		if ophase.Name != observedPhase.Name {
			continue
		}
		// Digest is computed once at resolution time (before controller mutations)
		// and is immutable; carry it forward so the full-struct replacement below
		// does not drop the required field.
		observedPhase.Digest = ophase.Digest
		// Preserve CompletedAt once set.
		if !ophase.CompletedAt.IsZero() {
			observedPhase.CompletedAt = ophase.CompletedAt
		}
		if observedPhase.Status == ocv1.PhaseStatusReady && observedPhase.CompletedAt.IsZero() {
			observedPhase.CompletedAt = now
		}
		// LastTransitionTime advances only when Status changes.
		if ophase.Status != observedPhase.Status {
			observedPhase.LastTransitionTime = now
		} else {
			observedPhase.LastTransitionTime = ophase.LastTransitionTime
		}
		objSet.Status.ObservedPhases[i] = observedPhase
		return
	}
	// First time this phase appears in the status.
	if observedPhase.Status == ocv1.PhaseStatusReady && observedPhase.CompletedAt.IsZero() {
		observedPhase.CompletedAt = now
	}
	observedPhase.LastTransitionTime = now
	objSet.Status.ObservedPhases = append(objSet.Status.ObservedPhases, observedPhase)
}

// HasSupersededObject reports whether any object in the phase has been adopted
// by a newer sibling revision (machinery.ActionProgressed). boxcutter only emits
// ActionProgressed for a newer sibling (a foreign owner is a collision), so no
// revision comparison is needed here.
func HasSupersededObject(pres machinery.PhaseResult) bool {
	for _, ores := range pres.GetObjects() {
		if ores.Action() == machinery.ActionProgressed {
			return true
		}
	}
	return false
}

// objectStatus returns details describing why obj is not yet complete.
func objectStatus(obj machinery.ObjectResult) ocv1.ObjectStatus {
	ref := types.ToObjectRef(obj.Object())
	objStatus := ocv1.ObjectStatus{
		Group:     ref.Group,
		Version:   ref.Version,
		Kind:      ref.Kind,
		Namespace: ref.Namespace,
		Name:      ref.Name,
	}

	if obj.IsPaused() && obj.Action() != machinery.ActionIdle {
		objStatus.Messages = pausedObjectMessages(obj)
		return objStatus
	}

	var msgs []string

	if obj.Action() == machinery.ActionCollision {
		msgs = append(msgs, "object ownership collision")
	}

	probes := obj.ProbeResults()
	for _, probeType := range slices.Sorted(maps.Keys(probes)) {
		result := probes[probeType]
		if result.Status == types.ProbeStatusTrue {
			continue
		}
		for _, m := range result.Messages {
			msgs = append(msgs, truncateMessage(fmt.Sprintf("%s: %s", probeType, m)))
		}
		if len(result.Messages) == 0 {
			msgs = append(msgs, truncateMessage(fmt.Sprintf("%s: %s", probeType, result.Status)))
		}
	}

	if len(msgs) == 0 {
		msgs = append(msgs, "not yet complete")
	} else if len(msgs) > 18 {
		msgs = msgs[:17]
		msgs = append(msgs, "additional objectDetails messages were removed for size constraints")
	}

	objStatus.Messages = msgs
	return objStatus
}

// pausedObjectMessages returns messages describing why a paused object is not
// idle, including field-level diff details when available.
func pausedObjectMessages(obj machinery.ObjectResult) []string {
	var summary string
	switch obj.Action() {
	case machinery.ActionCreated:
		return []string{"object does not exist"}
	case machinery.ActionRecovered:
		summary = "object was modified by another actor"
	case machinery.ActionCollision:
		summary = "object ownership collision"
	default:
		summary = "object content has changed"
	}

	if details := compareDetails(obj); len(details) > 0 {
		summary += "\n" + strings.Join(details, "\n")
	}
	return []string{truncateMessage(summary)}
}

// compareResulter is implemented by ObjectResult types that expose field-level
// comparison results.
type compareResulter interface {
	CompareResult() machinery.CompareResult
}

// compareDetails extracts human-readable field diff lines (added, modified,
// removed) from obj's CompareResult, if obj implements compareResulter.
func compareDetails(obj machinery.ObjectResult) []string {
	cr, ok := obj.(compareResulter)
	if !ok {
		return nil
	}
	comp := cr.CompareResult().Comparison
	if comp == nil {
		return nil
	}
	var details []string
	if comp.Added != nil && !comp.Added.Empty() {
		comp.Added.Leaves().Iterate(func(p fieldpath.Path) {
			details = append(details, fmt.Sprintf(" - added: %s", p))
		})
	}
	if comp.Modified != nil && !comp.Modified.Empty() {
		comp.Modified.Leaves().Iterate(func(p fieldpath.Path) {
			details = append(details, fmt.Sprintf(" - modified: %s", p))
		})
	}
	if comp.Removed != nil && !comp.Removed.Empty() {
		comp.Removed.Leaves().Iterate(func(p fieldpath.Path) {
			details = append(details, fmt.Sprintf(" - removed: %s", p))
		})
	}
	return details
}

// isRecoverablePhaseValidation reports whether a PhaseValidationError represents a
// recoverable precondition failure — specifically, a dry-run rejection caused by RBAC
// not yet propagated (Forbidden/Unauthorized) or a dependency CRD not yet registered
// (NoMatch). The controller retries automatically; no spec change is required.
// Returns false for permanent failures (invalid phase name, schema violations,
// duplicate objects) or any mixture of recoverable and permanent errors.
func isRecoverablePhaseValidation(verr *validation.PhaseValidationError) bool {
	if verr == nil || verr.PhaseError != nil || len(verr.Objects) == 0 {
		return false
	}
	for _, obj := range verr.Objects {
		for _, e := range obj.Errors {
			if !isRecoverableObjectError(e) {
				return false
			}
		}
	}
	return true
}

// isRecoverableObjectError reports whether an object-level error is a recoverable
// dry-run rejection: Forbidden or Unauthorized (RBAC not yet propagated) or a
// NoMatch error (dependency CRD not yet registered). errors.As traverses the unwrap
// chain, so this works whether or not the error is further wrapped in a
// validation.DryRunValidationError.
func isRecoverableObjectError(err error) bool {
	return apierrors.IsForbidden(err) ||
		apierrors.IsUnauthorized(err) ||
		apimeta.IsNoMatchError(err)
}

const maxMessageLength = 1024

// truncateMessage truncates s to at most maxMessageLength runes to fit within the CRD limit.
func truncateMessage(s string) string {
	r := []rune(s)
	if len(r) <= maxMessageLength {
		return s
	}
	return string(r[:maxMessageLength])
}
