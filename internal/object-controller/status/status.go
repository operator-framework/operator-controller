package status

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"pkg.package-operator.run/boxcutter/machinery"

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
			isSuperseded  = (HasSupersededObject(pres) && pres.IsComplete()) || pres.HasProgressed()
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
			// TODO: collect assertion failures and add to status
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

const maxMessageLength = 1024

// truncateMessage truncates s to at most maxMessageLength runes to fit within the CRD limit.
func truncateMessage(s string) string {
	r := []rune(s)
	if len(r) <= maxMessageLength {
		return s
	}
	return string(r[:maxMessageLength])
}
