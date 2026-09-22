package revision

import (
	"context"

	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

type ThreeTierEngine struct {
	revision            *boxcutter.RevisionEngine
	phase               *machinery.PhaseEngine
	reader              client.Reader
	completedPhaseNames map[string]bool
}

// New returns a new 3-Tier Boxcutter revision reconciler that extends the behavior of the default
// Boxcutter Engine to provide reconciliation results for each of the phases by:
// - Executing the standard revision reconciliation process and collecting results for phases up to the first probe failure
// - Reconciling previously completed phases to correct any drift in phase object spec
// - Reconciles remaining phases in read-only mode (with the WithPaused option) gathering status information
func New(observedPhases []ocv1.ObservedPhase, opts boxcutter.RevisionEngineOptions) (*ThreeTierEngine, error) {
	re, err := boxcutter.NewRevisionEngine(opts)
	if err != nil {
		return nil, err
	}
	pe, err := boxcutter.NewPhaseEngine(opts)
	if err != nil {
		return nil, err
	}
	return &ThreeTierEngine{
		revision:            re,
		phase:               pe,
		reader:              opts.Reader,
		completedPhaseNames: completedPhaseNames(observedPhases),
	}, nil
}

func (e *ThreeTierEngine) Reconcile(ctx context.Context, rev types.Revision, opts ...types.RevisionReconcileOption) (machinery.RevisionResult, error) {
	gatedResult, err := e.revision.Reconcile(ctx, rev, opts...)
	if err != nil {
		return gatedResult, err
	}
	if gatedResult.GetValidationError() != nil || gatedResult.HasProgressed() {
		return gatedResult, nil
	}

	gatedPhaseNames := make(map[string]struct{}, len(gatedResult.GetPhases()))
	for _, pr := range gatedResult.GetPhases() {
		gatedPhaseNames[pr.GetName()] = struct{}{}
	}

	var revOpts types.RevisionReconcileOptions
	for _, o := range opts {
		o.ApplyToRevisionReconcileOptions(&revOpts)
	}

	driftPhases, readOnlyPhases := splitPhases(rev, gatedPhaseNames, e.completedPhaseNames)

	var driftResults []machinery.PhaseResult
	var driftErr error
	for _, phase := range driftPhases {
		phaseOpts := revOpts.ForPhase(phase.GetName())
		pr, pErr := e.phase.Reconcile(ctx, rev.GetRevisionNumber(), phase, phaseOpts...) //nolint:staticcheck
		if pr != nil {                                                                   //nolint:staticcheck // defensive: boxcutter may return nil in future versions
			driftResults = append(driftResults, pr)
		}
		if pErr != nil {
			driftErr = pErr
			break
		}
	}

	var readOnlyResults []machinery.PhaseResult
	if driftErr == nil {
		for _, phase := range readOnlyPhases {
			phaseOpts := append(revOpts.ForPhase(phase.GetName()), types.WithPaused{})
			pr, pErr := e.phase.Reconcile(ctx, rev.GetRevisionNumber(), phase, phaseOpts...) //nolint:staticcheck
			if pr != nil {                                                                   //nolint:staticcheck // defensive: boxcutter may return nil in future versions
				readOnlyResults = append(readOnlyResults, pr)
			}
			if pErr != nil {
				break
			}
		}
	}

	return &result{
		gated:           gatedResult,
		driftResults:    driftResults,
		readOnlyResults: readOnlyResults,
	}, driftErr
}

func (e *ThreeTierEngine) Teardown(ctx context.Context, rev types.Revision, opts ...types.RevisionTeardownOption) (machinery.RevisionTeardownResult, error) {
	result, err := e.revision.Teardown(ctx, rev, opts...)
	if err != nil || result == nil || len(result.GetWaitingPhaseNames()) == 0 {
		return result, err
	}

	waitingNames := make(map[string]struct{}, len(result.GetWaitingPhaseNames()))
	for _, name := range result.GetWaitingPhaseNames() {
		waitingNames[name] = struct{}{}
	}

	var readOnlyPhases []machinery.PhaseTeardownResult
	for _, phase := range rev.GetPhases() {
		if _, ok := waitingNames[phase.GetName()]; !ok {
			continue
		}
		var present []types.ObjectRef
		for _, obj := range phase.GetObjects() {
			actual := obj.DeepCopyObject().(client.Object)
			if getErr := e.reader.Get(ctx, client.ObjectKeyFromObject(actual), actual); getErr == nil {
				present = append(present, types.ToObjectRef(actual))
			}
		}
		readOnlyPhases = append(readOnlyPhases, &readOnlyPhaseTeardownResult{
			name:    phase.GetName(),
			waiting: present,
		})
	}

	return &teardownResultWithReadOnly{
		RevisionTeardownResult: result,
		readOnlyPhases:         readOnlyPhases,
	}, nil
}

func completedPhaseNames(observedPhases []ocv1.ObservedPhase) map[string]bool {
	m := make(map[string]bool, len(observedPhases))
	for _, op := range observedPhases {
		if !op.CompletedAt.IsZero() {
			m[op.Name] = true
		}
	}
	return m
}

func splitPhases(rev types.Revision, gatedPhaseNames map[string]struct{}, completedPhases map[string]bool) ([]types.Phase, []types.Phase) {
	var drift, readOnly []types.Phase
	sawCompleted := false
	for _, phase := range rev.GetPhases() {
		if _, inGated := gatedPhaseNames[phase.GetName()]; inGated {
			continue
		}
		isCompleted := completedPhases[phase.GetName()]
		if !isCompleted && !sawCompleted {
			readOnly = append(readOnly, phase)
			readOnly = append(readOnly, phasesAfter(rev, gatedPhaseNames, phase.GetName())...)
			return drift, readOnly
		}
		sawCompleted = true
		drift = append(drift, phase)
		if !isCompleted {
			readOnly = append(readOnly, phasesAfter(rev, gatedPhaseNames, phase.GetName())...)
			return drift, readOnly
		}
	}
	return drift, readOnly
}

// phasesAfter returns all non-gated phases that follow the phase named afterName in the revision's phase order.
func phasesAfter(rev types.Revision, gatedPhaseNames map[string]struct{}, afterName string) []types.Phase {
	var result []types.Phase
	found := false
	for _, phase := range rev.GetPhases() {
		if phase.GetName() == afterName {
			found = true
			continue
		}
		if !found {
			continue
		}
		if _, inGated := gatedPhaseNames[phase.GetName()]; inGated {
			continue
		}
		result = append(result, phase)
	}
	return result
}
