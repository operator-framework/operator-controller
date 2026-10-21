package status

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"pkg.package-operator.run/boxcutter/machinery"
	boxcuttertypes "pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/validation"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestFromReconcile(t *testing.T) {
	now := metav1.Now()

	t.Run("incomplete phase with collision object is present but not synced", func(t *testing.T) {
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
				}},
			},
		}
		FromReconcile(&cos, result, now)
		require.NotNil(t, cos.Status.ObservedPhases)
		assert.Equal(t, ocv1.ObjectCounts{Total: 1, Present: 1}, cos.Status.ObservedPhases[0].ObjectCounts)
		assert.Equal(t, ocv1.PhaseStatusReconciling, cos.Status.ObservedPhases[0].Status)
	})

	t.Run("progressed marks all phases superseded and preserves total counts", func(t *testing.T) {
		cos := cosWithPhaseObjects(
			struct {
				name    string
				objects int
			}{"p1", 2},
			struct {
				name    string
				objects int
			}{"p2", 3},
		)
		result := &fakeRevisionResult{progressed: true}
		FromReconcile(&cos, result, now)
		require.Len(t, cos.Status.ObservedPhases, 2)
		assert.Equal(t, ocv1.PhaseStatusSuperseded, cos.Status.ObservedPhases[0].Status)
		assert.Equal(t, int64(2), cos.Status.ObservedPhases[0].ObjectCounts.Total)
		assert.Equal(t, ocv1.PhaseStatusSuperseded, cos.Status.ObservedPhases[1].Status)
		assert.Equal(t, int64(3), cos.Status.ObservedPhases[1].ObjectCounts.Total)
	})
}

func TestFromTeardown(t *testing.T) {
	now := metav1.Now()

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
		FromTeardown(&cos, result, now)
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
		FromTeardown(&cos, result, now)
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
		FromTeardown(&cos, result, now)
		require.NotNil(t, cos.Status.ObservedPhases)

		phases := cos.Status.ObservedPhases
		require.Len(t, phases, 3)

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

func cosSpecWithPhaseObjects(phaseDefs ...struct {
	name    string
	objects int
}) ocv1.ClusterObjectSetSpec {
	phases := make([]ocv1.ClusterObjectSetPhase, len(phaseDefs))
	for i, pd := range phaseDefs {
		objs := make([]ocv1.ClusterObjectSetObject, pd.objects)
		phases[i] = ocv1.ClusterObjectSetPhase{Name: pd.name, Objects: objs}
	}
	return ocv1.ClusterObjectSetSpec{
		Phases: phases,
	}
}

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

func TestPreserveCompletionTimes(t *testing.T) {
	now := metav1.NewTime(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))
	earlier := metav1.NewTime(now.Add(-time.Hour))

	t.Run("sets completedAt on Available phase with no completedAt", func(t *testing.T) {
		phases := []ocv1.ObservedPhase{
			{Name: "phase-1", Status: ocv1.PhaseStatusAvailable},
		}
		preserveCompletionTimes(&phases, now)
		require.NotNil(t, phases[0].CompletedAt)
		assert.Equal(t, now, *phases[0].CompletedAt)
	})

	t.Run("does not set completedAt on non-Available phase", func(t *testing.T) {
		phases := []ocv1.ObservedPhase{
			{Name: "phase-1", Status: ocv1.PhaseStatusReconciling},
		}
		preserveCompletionTimes(&phases, now)
		assert.Nil(t, phases[0].CompletedAt)
	})

	t.Run("does not overwrite existing completedAt on Available phase", func(t *testing.T) {
		phases := []ocv1.ObservedPhase{
			{Name: "phase-1", Status: ocv1.PhaseStatusAvailable, CompletedAt: &earlier},
		}
		preserveCompletionTimes(&phases, now)
		assert.Equal(t, earlier, *phases[0].CompletedAt)
	})
}

func TestTruncateMessage(t *testing.T) {
	t.Run("short message unchanged", func(t *testing.T) {
		assert.Equal(t, "hello", truncateMessage("hello"))
	})

	t.Run("message at limit unchanged", func(t *testing.T) {
		s := strings.Repeat("a", maxMessageLength)
		assert.Equal(t, s, truncateMessage(s))
	})

	t.Run("message over limit truncated to maxMessageLength runes", func(t *testing.T) {
		s := strings.Repeat("a", maxMessageLength+10)
		result := truncateMessage(s)
		assert.Len(t, []rune(result), maxMessageLength)
	})
}

func TestBuildObservedPhases(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	obj.SetName("my-cm")

	spec := cosSpecWithPhaseObjects(
		struct {
			name    string
			objects int
		}{"phase-1", 2},
		struct {
			name    string
			objects int
		}{"phase-2", 3},
	)

	t.Run("complete phase gets full counts and Available status", func(t *testing.T) {
		results := []machinery.PhaseResult{
			&fakePhaseResult{name: "phase-1", complete: true},
		}
		var observed []ocv1.ObservedPhase
		buildObservedPhases(spec.Phases, &observed, results)
		assert.Equal(t, ocv1.PhaseStatusAvailable, observed[0].Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 2, Present: 2, Synced: 2, Ready: 2}, observed[0].ObjectCounts)
	})

	t.Run("unevaluated phase gets Unknown status and total only", func(t *testing.T) {
		var observed []ocv1.ObservedPhase
		buildObservedPhases(spec.Phases, &observed, nil)
		assert.Equal(t, ocv1.PhaseStatusUnknown, observed[0].Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 2}, observed[0].ObjectCounts)
	})

	t.Run("phase with all synced objects reports WaitingForAssertions", func(t *testing.T) {
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
		var observed []ocv1.ObservedPhase
		buildObservedPhases(spec.Phases, &observed, results)
		assert.Equal(t, ocv1.PhaseStatusWaitingForAssertions, observed[0].Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 2, Present: 2, Synced: 2, Ready: 1}, observed[0].ObjectCounts)
	})

	t.Run("phase with collision object reports Reconciling", func(t *testing.T) {
		results := []machinery.PhaseResult{
			&fakePhaseResult{
				name:     "phase-1",
				complete: false,
				objects: []machinery.ObjectResult{
					&fakeCollisionResult{fakeObjectResult: fakeObjectResult{obj: obj, complete: false, probes: boxcuttertypes.ProbeResultContainer{}}},
				},
			},
		}
		var observed []ocv1.ObservedPhase
		buildObservedPhases(spec.Phases, &observed, results)
		assert.Equal(t, ocv1.PhaseStatusReconciling, observed[0].Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 2, Present: 1}, observed[0].ObjectCounts)
	})

	t.Run("phase with paused objects reports Pending and tracks details", func(t *testing.T) {
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
		var observed []ocv1.ObservedPhase
		buildObservedPhases(spec.Phases, &observed, results)
		require.Len(t, observed, 2)
		assert.Equal(t, ocv1.PhaseStatusPending, observed[1].Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 3, Present: 2, Synced: 1, Ready: 1}, observed[1].ObjectCounts)
	})
}
