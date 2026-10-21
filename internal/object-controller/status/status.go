package status

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/types"
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

// FromReconcile updates the status of the given ClusterObjectSet based on a reconcile result,
// populating the phase level and aggregate ObjectCounts.
func FromReconcile(cos *ocv1.ClusterObjectSet, result machinery.RevisionResult, now metav1.Time) {
	if cos == nil {
		return
	}
	observedPhasesFromReconcileResult(cos.Spec.Phases, &cos.Status.ObservedPhases, result)
	preserveCompletionTimes(&cos.Status.ObservedPhases, now)
	cos.Status.ObjectCounts = sumObjectCounts(cos.Status.ObservedPhases)
}

// observedPhasesFromReconcileResult populates observedPhases from a reconcile result.
// When the revision has progressed (i.e. is transitioning), only Total counts are preserved from existing
// observed phases; otherwise full per-object counts are derived from phase results.
func observedPhasesFromReconcileResult(specPhases []ocv1.ClusterObjectSetPhase, observedPhases *[]ocv1.ObservedPhase, result machinery.RevisionResult) {
	if observedPhases == nil || result == nil {
		return
	}
	if result.HasProgressed() {
		allPhasesWithStatus(specPhases, observedPhases, ocv1.PhaseStatusSuperseded)
	} else {
		buildObservedPhases(specPhases, observedPhases, result.GetPhases())
	}
}

// allPhasesWithStatus sets every observed phase to status and updates each phase's
// object count total from specPhases. Used when a revision is progressing and
// detailed per-object results are not yet available.
func allPhasesWithStatus(specPhases []ocv1.ClusterObjectSetPhase, observedPhases *[]ocv1.ObservedPhase, status ocv1.PhaseStatus) {
	objTotalCountMap := make(map[string]int)
	for _, specPhase := range specPhases {
		objTotalCountMap[specPhase.Name] = len(specPhase.Objects)
	}
	op := *observedPhases
	for i := range op {
		op[i].ObjectCounts.Total = int64(objTotalCountMap[op[i].Name])
		op[i].Status = status
		op[i].Message = ""
		op[i].ObjectDetails = nil
	}
	*observedPhases = op
}

// buildObservedPhases constructs observed phases from reconcile phase results.
// Complete phases receive full counts (Total=Present=Synced=Ready); incomplete phases are handled by incompletePhase.
func buildObservedPhases(specPhases []ocv1.ClusterObjectSetPhase, observedPhases *[]ocv1.ObservedPhase, phaseResults []machinery.PhaseResult) {
	mapSpecPhases(specPhases, observedPhases, phaseResults, ocv1.PhaseStatusReady, "Phase was not evaluated",
		func(total int64) ocv1.ObjectCounts {
			return ocv1.ObjectCounts{Total: total, Present: total, Synced: total, Ready: total}
		},
		incompletePhase,
	)
}

// incompletePhase computes ObjectCounts for a phase whose reconcile result is not yet complete.
// Present counts all objects that exist on the cluster (non-paused, or paused but not freshly created).
// Synced counts objects that are idle (no pending changes). Ready counts objects reported as complete.
func incompletePhase(sp ocv1.ClusterObjectSetPhase, pr machinery.PhaseResult, observedPhase *ocv1.ObservedPhase) {
	observedPhase.ObjectCounts = ocv1.ObjectCounts{Total: int64(len(sp.Objects))}

	paused := false
	objectDetails := make([]ocv1.ObjectStatus, 0, len(pr.GetObjects()))
	for _, obj := range pr.GetObjects() {
		if !obj.IsPaused() || obj.Action() != machinery.ActionCreated {
			observedPhase.ObjectCounts.Present++
		}
		if obj.IsPaused() {
			paused = true
			if obj.Action() == machinery.ActionIdle {
				observedPhase.ObjectCounts.Synced++
			}
		} else {
			if obj.Action() == machinery.ActionIdle {
				observedPhase.ObjectCounts.Synced++
			}
		}
		if obj.IsComplete() {
			observedPhase.ObjectCounts.Ready++
			continue
		}
		os := objectStatusFromRef(types.ToObjectRef(obj.Object()))
		os.Messages = objectMessages(obj)
		objectDetails = append(objectDetails, os)
	}
	observedPhase.ObjectDetails = objectDetails

	switch {
	case paused:
		observedPhase.Status = ocv1.PhaseStatusPending
		observedPhase.Message = "Waiting for earlier phases to complete"
	case observedPhase.ObjectCounts.Synced == observedPhase.ObjectCounts.Total:
		observedPhase.Status = ocv1.PhaseStatusWaitingForAssertions
	default:
		observedPhase.Status = ocv1.PhaseStatusReconciling
	}
}

// FromTeardown updates the status of the given ClusterObjectSet based on a teardown result,
// populating ObservedPhases and the aggregate ObjectCounts.
func FromTeardown(cos *ocv1.ClusterObjectSet, result machinery.RevisionTeardownResult, now metav1.Time) {
	if cos == nil {
		return
	}
	observedPhasesFromTeardownResult(cos.Spec.Phases, &cos.Status.ObservedPhases, result)
	preserveCompletionTimes(&cos.Status.ObservedPhases, now)
	cos.Status.ObjectCounts = sumObjectCounts(cos.Status.ObservedPhases)
}

// observedPhasesFromTeardownResult populates observedPhases from a teardown result.
func observedPhasesFromTeardownResult(specPhases []ocv1.ClusterObjectSetPhase, observedPhases *[]ocv1.ObservedPhase, result machinery.RevisionTeardownResult) {
	if result == nil || observedPhases == nil {
		return
	}
	activeName, _ := result.GetActivePhaseName()
	buildTeardownObservedPhases(specPhases, observedPhases, result.GetPhases(), activeName)
}

// buildTeardownObservedPhases constructs observed phases from teardown phase results.
// Complete phases receive only the Total count; incomplete phases are handled by tearingDownPhase.
func buildTeardownObservedPhases(specPhases []ocv1.ClusterObjectSetPhase, observedPhases *[]ocv1.ObservedPhase, phaseResults []machinery.PhaseTeardownResult, activePhaseName string) {
	mapSpecPhases(specPhases, observedPhases, phaseResults, ocv1.PhaseStatusTeardownComplete, "",
		func(total int64) ocv1.ObjectCounts {
			return ocv1.ObjectCounts{Total: total}
		},
		func(sp ocv1.ClusterObjectSetPhase, pr machinery.PhaseTeardownResult, op *ocv1.ObservedPhase) {
			tearingDownPhase(sp, pr, pr.GetName() == activePhaseName, op)
		},
	)
}

// tearingDownPhase sets ObjectCounts for a phase that is still being torn down.
// Total is derived from the spec; Present reflects objects still waiting to be deleted.
func tearingDownPhase(sp ocv1.ClusterObjectSetPhase, pr machinery.PhaseTeardownResult, active bool, op *ocv1.ObservedPhase) {
	if op == nil {
		return
	}
	op.ObjectCounts = ocv1.ObjectCounts{
		Total:   int64(len(sp.Objects)),
		Present: int64(len(pr.Waiting())),
	}

	objectDetails := make([]ocv1.ObjectStatus, 0, len(pr.Waiting()))
	if active {
		op.Status = ocv1.PhaseStatusTearingDown
	} else {
		op.Status = ocv1.PhaseStatusPending
		op.Message = "Waiting for later phases to complete teardown"
	}
	for _, ref := range pr.Waiting() {
		os := objectStatusFromRef(ref)
		os.Messages = []string{"awaiting deletion"}
		objectDetails = append(objectDetails, os)
	}
	op.ObjectDetails = objectDetails
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
	observedPhases *[]ocv1.ObservedPhase,
	results []T,
	completeStatus ocv1.PhaseStatus,
	unevaluatedMessage string,
	completeObjectCounts func(total int64) ocv1.ObjectCounts,
	buildIncomplete func(ocv1.ClusterObjectSetPhase, T, *ocv1.ObservedPhase),
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
		// Digest is not set here, despite it being a required field, as by this
		// point the objects have already been mutated by the controller and
		// their digests can no longer be computed.
		if _, initialized := observedPhasesMap[sp.Name]; !initialized {
			observedPhasesMap[sp.Name] = &ocv1.ObservedPhase{Name: sp.Name}
		}
		observedPhasesMap[sp.Name].Message = ""
		if !evaluated {
			observedPhasesMap[sp.Name].Status = ocv1.PhaseStatusUnknown
			observedPhasesMap[sp.Name].Message = truncateMessage(unevaluatedMessage)
			observedPhasesMap[sp.Name].ObjectCounts = ocv1.ObjectCounts{Total: total}
			observedPhasesMap[sp.Name].ObjectDetails = nil
			continue
		}
		if r.IsComplete() {
			observedPhasesMap[sp.Name].Status = completeStatus
			observedPhasesMap[sp.Name].ObjectCounts = completeObjectCounts(total)
			observedPhasesMap[sp.Name].ObjectDetails = nil
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

// objectStatusFromRef constructs an ObjectStatus identifying the object by
// group, version, kind, namespace, and name.
func objectStatusFromRef(ref types.ObjectRef) ocv1.ObjectStatus {
	return ocv1.ObjectStatus{
		Group:     ref.Group,
		Version:   ref.Version,
		Kind:      ref.Kind,
		Namespace: ref.Namespace,
		Name:      ref.Name,
	}
}

// objectMessages returns messages describing why obj is not yet complete,
// delegating to pausedObjectMessages for paused non-idle objects.
func objectMessages(obj machinery.ObjectResult) []string {
	if obj.IsPaused() && obj.Action() != machinery.ActionIdle {
		return pausedObjectMessages(obj)
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
	}

	return msgs
}

// preserveCompletionTimes sets CompletedAt to now on any phase that does not
// already have a completion time recorded.
func preserveCompletionTimes(observedPhases *[]ocv1.ObservedPhase, now metav1.Time) {
	if observedPhases == nil {
		return
	}
	for i := range *observedPhases {
		if (*observedPhases)[i].CompletedAt == nil && (*observedPhases)[i].Status == ocv1.PhaseStatusReady {
			(*observedPhases)[i].CompletedAt = &now
		}
	}
}

// compareResulter is implemented by ObjectResult types that expose field-level
// comparison results.
type compareResulter interface {
	CompareResult() machinery.CompareResult
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
	default:
		summary = "object content has changed"
	}

	if details := compareDetails(obj); len(details) > 0 {
		summary += "\n" + strings.Join(details, "\n")
	}
	return []string{truncateMessage(summary)}
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

const maxMessageLength = 1024

// truncateMessage truncates s to at most maxMessageLength runes to fit within the CRD limit.
func truncateMessage(s string) string {
	r := []rune(s)
	if len(r) <= maxMessageLength {
		return s
	}
	return string(r[:maxMessageLength])
}
