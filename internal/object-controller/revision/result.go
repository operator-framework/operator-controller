package revision

import (
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/validation"
)

type result struct {
	gated           machinery.RevisionResult
	driftResults    []machinery.PhaseResult
	readOnlyResults []machinery.PhaseResult
}

func (r *result) GetValidationError() *validation.RevisionValidationError {
	return r.gated.GetValidationError()
}

func (r *result) GetPhases() []machinery.PhaseResult {
	result := append(r.gated.GetPhases(), r.driftResults...)
	return append(result, r.readOnlyResults...)
}

func (r *result) InTransition() bool {
	if r.gated.InTransition() {
		return true
	}
	for _, dr := range r.driftResults {
		if !dr.IsComplete() {
			return true
		}
	}
	return false
}

func (r *result) IsComplete() bool {
	if !r.gated.IsComplete() {
		return false
	}
	for _, dr := range r.driftResults {
		if !dr.IsComplete() {
			return false
		}
	}
	return true
}

func (r *result) HasProgressed() bool {
	return r.gated.HasProgressed()
}

func (r *result) String() string {
	return r.gated.String()
}

type teardownResultWithReadOnly struct {
	machinery.RevisionTeardownResult
	readOnlyPhases []machinery.PhaseTeardownResult
}

func (r *teardownResultWithReadOnly) GetPhases() []machinery.PhaseTeardownResult {
	return append(r.RevisionTeardownResult.GetPhases(), r.readOnlyPhases...)
}

func (r *teardownResultWithReadOnly) GetWaitingPhaseNames() []string {
	return nil
}

type readOnlyPhaseTeardownResult struct {
	name    string
	waiting []types.ObjectRef
}

func (r *readOnlyPhaseTeardownResult) GetName() string            { return r.name }
func (r *readOnlyPhaseTeardownResult) IsComplete() bool           { return false }
func (r *readOnlyPhaseTeardownResult) Gone() []types.ObjectRef    { return nil }
func (r *readOnlyPhaseTeardownResult) Waiting() []types.ObjectRef { return r.waiting }
func (r *readOnlyPhaseTeardownResult) String() string             { return r.name }
