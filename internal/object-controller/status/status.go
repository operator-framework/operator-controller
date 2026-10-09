package status

import (
	"pkg.package-operator.run/boxcutter/machinery"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// sumObjectCounts aggregates ObjectCounts across all observed phases into a single total.
// Returns nil if phases is empty.
func sumObjectCounts(phases []ocv1.ObservedPhase) *ocv1.ObjectCounts {
	if len(phases) == 0 {
		return nil
	}
	var counts ocv1.ObjectCounts
	for i := range phases {
		counts.Total += phases[i].ObjectCounts.Total
		counts.Present += phases[i].ObjectCounts.Present
		counts.Synced += phases[i].ObjectCounts.Synced
		counts.Available += phases[i].ObjectCounts.Available
	}
	return &counts
}

// FromReconcile updates the status of the given ClusterObjectSet based on a reconcile result,
// populating the phase level and aggregate ObjectCounts.
func FromReconcile(cos *ocv1.ClusterObjectSet, result machinery.RevisionResult) {
	if cos == nil {
		return
	}
	observedPhasesFromReconcileResult(cos.Spec.Phases, result, &cos.Status.ObservedPhases)
	cos.Status.ObjectCounts = sumObjectCounts(cos.Status.ObservedPhases)
}

// observedPhasesFromReconcileResult populates observedPhases from a reconcile result.
// When the revision has progressed (i.e. is transitioning), only Total counts are preserved from existing
// observed phases; otherwise full per-object counts are derived from phase results.
func observedPhasesFromReconcileResult(specPhases []ocv1.ClusterObjectSetPhase, result machinery.RevisionResult, observedPhases *[]ocv1.ObservedPhase) {
	if result == nil || observedPhases == nil {
		return
	}
	if result.HasProgressed() {
		allPhasesWithCounts(specPhases, observedPhases)
	} else {
		buildObservedPhases(specPhases, result.GetPhases(), observedPhases)
	}
}

// allPhasesWithCounts resets each existing observed phase's ObjectCounts to only Total, derived from
// the spec phase object list. Used when a revision is progressing and detailed per-object results are not yet available.
func allPhasesWithCounts(specPhases []ocv1.ClusterObjectSetPhase, observedPhases *[]ocv1.ObservedPhase) {
	objTotalCountMap := make(map[string]int)
	for _, specPhase := range specPhases {
		objTotalCountMap[specPhase.Name] = len(specPhase.Objects)
	}
	op := *observedPhases
	for i := range op {
		op[i].ObjectCounts = ocv1.ObjectCounts{Total: int64(objTotalCountMap[op[i].Name])}
	}
	*observedPhases = op
}

// buildObservedPhases constructs observed phases from reconcile phase results.
// Complete phases receive full counts (Total=Present=Synced=Available); incomplete phases are handled by incompletePhase.
func buildObservedPhases(specPhases []ocv1.ClusterObjectSetPhase, phaseResults []machinery.PhaseResult, observedPhases *[]ocv1.ObservedPhase) {
	mapSpecPhases(specPhases, phaseResults,
		func(total int64) ocv1.ObjectCounts {
			return ocv1.ObjectCounts{Total: total, Present: total, Synced: total, Available: total}
		},
		incompletePhase,
		observedPhases,
	)
}

// incompletePhase computes ObjectCounts for a phase whose reconcile result is not yet complete.
// Present counts all objects that exist on the cluster (non-paused, or paused but not freshly created).
// Synced counts objects that are idle (no pending changes). Available counts objects reported as complete.
func incompletePhase(sp ocv1.ClusterObjectSetPhase, pr machinery.PhaseResult, observedPhase *ocv1.ObservedPhase) {
	observedPhase.ObjectCounts = ocv1.ObjectCounts{Total: int64(len(sp.Objects))}
	for _, obj := range pr.GetObjects() {
		if !obj.IsPaused() || obj.Action() != machinery.ActionCreated {
			observedPhase.ObjectCounts.Present++
		}
		if obj.IsPaused() {
			if obj.Action() == machinery.ActionIdle {
				observedPhase.ObjectCounts.Synced++
			}
		} else {
			switch obj.Action() {
			case machinery.ActionIdle, machinery.ActionUpdated, machinery.ActionCreated, machinery.ActionRecovered:
				observedPhase.ObjectCounts.Synced++
			}
		}
		if obj.IsComplete() {
			observedPhase.ObjectCounts.Available++
			continue
		}
	}
}

// FromTeardown updates the status of the given ClusterObjectSet based on a teardown result,
// populating ObservedPhases and the aggregate ObjectCounts.
func FromTeardown(cos *ocv1.ClusterObjectSet, result machinery.RevisionTeardownResult) {
	if cos == nil {
		return
	}
	observedPhasesFromTeardownResult(cos.Spec.Phases, result, &cos.Status.ObservedPhases)
	cos.Status.ObjectCounts = sumObjectCounts(cos.Status.ObservedPhases)
}

// observedPhasesFromTeardownResult populates observedPhases from a teardown result.
func observedPhasesFromTeardownResult(specPhases []ocv1.ClusterObjectSetPhase, result machinery.RevisionTeardownResult, observedPhases *[]ocv1.ObservedPhase) {
	if result == nil || observedPhases == nil {
		return
	}
	buildTeardownObservedPhases(specPhases, result.GetPhases(), observedPhases)
}

// buildTeardownObservedPhases constructs observed phases from teardown phase results.
// Complete phases receive only the Total count; incomplete phases are handled by tearingDownPhase.
func buildTeardownObservedPhases(specPhases []ocv1.ClusterObjectSetPhase, phaseResults []machinery.PhaseTeardownResult, observedPhases *[]ocv1.ObservedPhase) {
	mapSpecPhases(specPhases, phaseResults,
		func(total int64) ocv1.ObjectCounts {
			return ocv1.ObjectCounts{Total: total}
		},
		tearingDownPhase,
		observedPhases,
	)
}

// tearingDownPhase sets ObjectCounts for a phase that is still being torn down.
// Total is derived from the spec; Present reflects objects still waiting to be deleted.
func tearingDownPhase(sp ocv1.ClusterObjectSetPhase, pr machinery.PhaseTeardownResult, op *ocv1.ObservedPhase) {
	op.ObjectCounts = ocv1.ObjectCounts{
		Total:   int64(len(sp.Objects)),
		Present: int64(len(pr.Waiting())),
	}
}

// mapSpecPhases merges spec phase definitions with evaluated phase results into observedPhases.
// For each spec phase: if no result exists, only Total is set; if the result is complete,
// completeObjectCounts is called; otherwise buildIncomplete fills in partial counts.
// The output slice is ordered to match specPhases.
func mapSpecPhases[T interface {
	GetName() string
	IsComplete() bool
}](
	specPhases []ocv1.ClusterObjectSetPhase,
	results []T,
	completeObjectCounts func(total int64) ocv1.ObjectCounts,
	buildIncomplete func(ocv1.ClusterObjectSetPhase, T, *ocv1.ObservedPhase),
	observedPhases *[]ocv1.ObservedPhase,
) {
	if observedPhases == nil {
		return
	}
	resultsByName := make(map[string]T, len(results))
	for _, r := range results {
		resultsByName[r.GetName()] = r
	}

	observedPhasesMap := make(map[string]*ocv1.ObservedPhase)
	for i := range *observedPhases {
		observedPhasesMap[(*observedPhases)[i].Name] = &(*observedPhases)[i]
	}

	for _, sp := range specPhases {
		total := int64(len(sp.Objects))
		r, evaluated := resultsByName[sp.Name]
		if _, initialized := observedPhasesMap[sp.Name]; !initialized {
			observedPhasesMap[sp.Name] = &ocv1.ObservedPhase{Name: sp.Name}
		}
		if !evaluated {
			observedPhasesMap[sp.Name].ObjectCounts = ocv1.ObjectCounts{Total: total}
			continue
		}
		if r.IsComplete() {
			observedPhasesMap[sp.Name].ObjectCounts = completeObjectCounts(total)
			continue
		}
		buildIncomplete(sp, r, observedPhasesMap[sp.Name])
	}

	result := make([]ocv1.ObservedPhase, 0, len(specPhases))
	for _, sp := range specPhases {
		if op, ok := observedPhasesMap[sp.Name]; ok {
			result = append(result, *op)
		}
	}
	*observedPhases = result
}
