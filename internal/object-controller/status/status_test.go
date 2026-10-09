package status

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"pkg.package-operator.run/boxcutter/machinery"
	boxcuttertypes "pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/validation"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestFromReconcile(t *testing.T) {
	t.Run("complete sets available and completed at", func(t *testing.T) {
		cos := cosWithPhaseObjects(
			struct {
				name    string
				objects int
			}{"p1", 1},
		)
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
		obj.SetName("created-cm")
		result := &fakeRevisionResult{
			complete: false,
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: false, objects: []machinery.ObjectResult{
					&fakeCollisionResult{
						fakeObjectResult: fakeObjectResult{obj: obj, complete: false, probes: boxcuttertypes.ProbeResultContainer{}},
					},
				},
				},
			},
		}
		FromReconcile(&cos, result)
		assert.Equal(t, ocv1.ObjectCounts{Total: 1, Present: 1}, cos.Status.ObservedPhases[0].ObjectCounts)
	})
}

func TestFromTeardown(t *testing.T) {

	t.Run("teardown in progress", func(t *testing.T) {
		cos := cosWithPhaseObjects(
			struct {
				name    string
				objects int
			}{"p1", 3},
		)
		result := &fakeTeardownResult{
			complete: false,
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p1", complete: false, waiting: []boxcuttertypes.ObjectRef{{}, {}}},
			},
		}
		FromTeardown(&cos, result)
		require.NotNil(t, cos.Status.ObservedPhases)
		assert.Equal(t, ocv1.ObjectCounts{Total: 3, Present: 2}, cos.Status.ObservedPhases[0].ObjectCounts)
	})

	t.Run("teardown complete sets all counts to zero except total", func(t *testing.T) {
		cos := cosWithPhaseObjects(
			struct {
				name    string
				objects int
			}{"p1", 5},
		)
		result := &fakeTeardownResult{
			complete: true,
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p1", complete: true},
			},
		}
		FromTeardown(&cos, result)
		require.NotNil(t, cos.Status.ObservedPhases)
		assert.Equal(t, ocv1.ObjectCounts{Total: 5}, cos.Status.ObservedPhases[0].ObjectCounts)
	})

	t.Run("read-only teardown phases included in GetPhases", func(t *testing.T) {
		cos := cosWithPhaseObjects(
			struct {
				name    string
				objects int
			}{"p1", 2},
			struct {
				name    string
				objects int
			}{"p2", 3},
			struct {
				name    string
				objects int
			}{"p3", 4},
		)
		result := &fakeTeardownResult{
			complete: false,
			active:   "p2",
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p3", complete: true},
				&fakePhaseTeardownResult{name: "p2", complete: false, waiting: []boxcuttertypes.ObjectRef{{}, {}}},
				&fakePhaseTeardownResult{name: "p1", complete: false, waiting: []boxcuttertypes.ObjectRef{{}, {}}},
			},
		}
		FromTeardown(&cos, result)
		require.NotNil(t, cos.Status.ObservedPhases)
		phases := cos.Status.ObservedPhases

		assert.Equal(t, ocv1.ObjectCounts{Total: 2, Present: 2}, phases[0].ObjectCounts)

		assert.Equal(t, ocv1.ObjectCounts{Total: 3, Present: 2}, phases[1].ObjectCounts)

		assert.Equal(t, ocv1.ObjectCounts{Total: 4}, phases[2].ObjectCounts)
	})
}

// Test fakes

type fakeRevisionResult struct {
	validationError *validation.RevisionValidationError
	phases          []machinery.PhaseResult
	progressed      bool
	complete        bool
}

func (f *fakeRevisionResult) GetValidationError() *validation.RevisionValidationError {
	return f.validationError
}
func (f *fakeRevisionResult) GetPhases() []machinery.PhaseResult { return f.phases }
func (f *fakeRevisionResult) InTransition() bool                 { return false }
func (f *fakeRevisionResult) IsComplete() bool                   { return f.complete }
func (f *fakeRevisionResult) HasProgressed() bool                { return f.progressed }
func (f *fakeRevisionResult) String() string                     { return "" }

type fakePhaseResult struct {
	name            string
	complete        bool
	objects         []machinery.ObjectResult
	validationError *validation.PhaseValidationError
}

func (f *fakePhaseResult) GetName() string                      { return f.name }
func (f *fakePhaseResult) IsComplete() bool                     { return f.complete }
func (f *fakePhaseResult) GetObjects() []machinery.ObjectResult { return f.objects }
func (f *fakePhaseResult) InTransition() bool                   { return false }
func (f *fakePhaseResult) HasProgressed() bool                  { return false }
func (f *fakePhaseResult) String() string                       { return f.name }
func (f *fakePhaseResult) GetValidationError() *validation.PhaseValidationError {
	return f.validationError
}

type fakeObjectResult struct {
	obj      machinery.Object
	complete bool
	paused   bool
	probes   boxcuttertypes.ProbeResultContainer
	action   machinery.Action
}

func (f *fakeObjectResult) Object() machinery.Object                          { return f.obj }
func (f *fakeObjectResult) IsComplete() bool                                  { return f.complete }
func (f *fakeObjectResult) IsPaused() bool                                    { return f.paused }
func (f *fakeObjectResult) ProbeResults() boxcuttertypes.ProbeResultContainer { return f.probes }
func (f *fakeObjectResult) Action() machinery.Action                          { return f.action }
func (f *fakeObjectResult) String() string                                    { return "" }

type fakeCollisionResult struct {
	fakeObjectResult
}

func (f *fakeCollisionResult) Action() machinery.Action { return machinery.ActionCollision }

type fakeTeardownResult struct {
	complete bool
	active   string
	phases   []machinery.PhaseTeardownResult
	waiting  []string
}

func (f *fakeTeardownResult) IsComplete() bool                           { return f.complete }
func (f *fakeTeardownResult) GetPhases() []machinery.PhaseTeardownResult { return f.phases }
func (f *fakeTeardownResult) GetActivePhaseName() (string, bool)         { return f.active, f.active != "" }
func (f *fakeTeardownResult) GetWaitingPhaseNames() []string             { return f.waiting }
func (f *fakeTeardownResult) GetGonePhaseNames() []string                { return nil }
func (f *fakeTeardownResult) String() string                             { return "" }

type fakePhaseTeardownResult struct {
	name     string
	complete bool
	waiting  []boxcuttertypes.ObjectRef
}

func (f *fakePhaseTeardownResult) GetName() string                     { return f.name }
func (f *fakePhaseTeardownResult) IsComplete() bool                    { return f.complete }
func (f *fakePhaseTeardownResult) Gone() []boxcuttertypes.ObjectRef    { return nil }
func (f *fakePhaseTeardownResult) Waiting() []boxcuttertypes.ObjectRef { return f.waiting }
func (f *fakePhaseTeardownResult) String() string                      { return f.name }

func cosWithPhaseObjects(phaseDefs ...struct {
	name    string
	objects int
}) ocv1.ClusterObjectSet {
	phases := make([]ocv1.ClusterObjectSetPhase, len(phaseDefs))
	observedPhases := make([]ocv1.ObservedPhase, len(phaseDefs))
	for i, pd := range phaseDefs {
		objs := make([]ocv1.ClusterObjectSetObject, pd.objects)
		phases[i] = ocv1.ClusterObjectSetPhase{Name: pd.name, Objects: objs}
		observedPhases[i] = ocv1.ObservedPhase{Name: pd.name}
	}
	return ocv1.ClusterObjectSet{
		Spec: ocv1.ClusterObjectSetSpec{
			Phases: phases,
		},
		Status: ocv1.ClusterObjectSetStatus{
			ObservedPhases: observedPhases,
		},
	}
}

func TestBuildObservedPhases_ReadOnly(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	obj.SetName("my-cm")

	spec := cosWithPhaseObjects(
		struct {
			name    string
			objects int
		}{"phase-1", 2},
		struct {
			name    string
			objects int
		}{"phase-2", 3},
	).Spec

	t.Run("read-only phase all objects idle and complete", func(t *testing.T) {
		results := []machinery.PhaseResult{
			&fakePhaseResult{name: "phase-1", complete: true},
			&fakePhaseResult{
				name:     "phase-2",
				complete: true,
				objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: true, paused: true, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: true, paused: true, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: true, paused: true, action: machinery.ActionIdle},
				},
			},
		}
		observedPhases := []ocv1.ObservedPhase{
			{Name: "phase-1"},
			{Name: "phase-2"},
		}
		buildObservedPhases(spec.Phases, results, &observedPhases)
		require.Len(t, observedPhases, 2)
		assert.Equal(t, ocv1.ObjectCounts{Total: 3, Present: 3, Synced: 3, Available: 3}, observedPhases[1].ObjectCounts)
	})

	t.Run("read-only phase with objects needing updates", func(t *testing.T) {
		results := []machinery.PhaseResult{
			&fakePhaseResult{name: "phase-1", complete: true},
			&fakePhaseResult{
				name:     "phase-2",
				complete: false,
				objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: true, paused: true, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: false, paused: true, action: machinery.ActionUpdated},
					&fakeObjectResult{obj: obj, complete: false, paused: true, action: machinery.ActionCreated},
				},
			},
		}
		observedPhases := []ocv1.ObservedPhase{
			{Name: "phase-1"},
			{Name: "phase-2"},
		}
		buildObservedPhases(spec.Phases, results, &observedPhases)
		require.Len(t, observedPhases, 2)
		assert.Equal(t, ocv1.ObjectCounts{Total: 3, Present: 2, Synced: 1, Available: 1}, observedPhases[1].ObjectCounts)
	})

	t.Run("active phase synced but one probe failing reports WaitingForAssertions", func(t *testing.T) {
		results := []machinery.PhaseResult{
			&fakePhaseResult{
				name:     "phase-1",
				complete: false,
				objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: true, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionUpdated},
				},
			},
		}
		observedPhases := []ocv1.ObservedPhase{
			{Name: "phase-1"},
		}
		buildObservedPhases(spec.Phases, results, &observedPhases)
		assert.Equal(t, ocv1.ObjectCounts{Total: 2, Present: 2, Synced: 2, Available: 1}, observedPhases[0].ObjectCounts)
	})

	t.Run("active phase fully synced but probes failing", func(t *testing.T) {
		results := []machinery.PhaseResult{
			&fakePhaseResult{
				name:     "phase-1",
				complete: false,
				objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: true, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionIdle},
				},
			},
		}
		observedPhases := []ocv1.ObservedPhase{
			{Name: "phase-1"},
		}
		buildObservedPhases(spec.Phases, results, &observedPhases)
		assert.Equal(t, ocv1.ObjectCounts{Total: 2, Present: 2, Synced: 2, Available: 1}, observedPhases[0].ObjectCounts)
	})

	t.Run("complete phase has all counts equal to total", func(t *testing.T) {
		results := []machinery.PhaseResult{
			&fakePhaseResult{name: "phase-1", complete: true},
		}
		observedPhases := []ocv1.ObservedPhase{
			{Name: "phase-1"},
		}
		buildObservedPhases(spec.Phases, results, &observedPhases)
		assert.Equal(t, ocv1.ObjectCounts{Total: 2, Present: 2, Synced: 2, Available: 2}, observedPhases[0].ObjectCounts)
	})

	t.Run("unknown phase has total but zero synced available and present", func(t *testing.T) {
		observedPhases := []ocv1.ObservedPhase{
			{Name: "phase-1"},
		}
		buildObservedPhases(spec.Phases, nil, &observedPhases)
		assert.Equal(t, ocv1.ObjectCounts{Total: 2}, observedPhases[0].ObjectCounts)
	})
}
