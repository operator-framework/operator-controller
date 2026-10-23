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
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v6/typed"

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

	t.Run("active TearingDown phase has correct status counts and no message", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 3})
		result := &fakeTeardownResult{
			complete: false,
			active:   "p1",
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p1", complete: false, waiting: []boxcuttertypes.ObjectRef{{}, {}}},
			},
		}
		FromTeardown(&cos, result, now)
		require.Len(t, cos.Status.ObservedPhases, 1)
		phase := cos.Status.ObservedPhases[0]
		assert.Equal(t, ocv1.PhaseStatusTearingDown, phase.Status)
		assert.Empty(t, phase.Message)
		assert.Equal(t, ocv1.ObjectCounts{Total: 3, Present: 2}, phase.ObjectCounts)
		assert.Len(t, phase.ObjectDetails, 2)
	})

	t.Run("teardown complete sets TeardownComplete status and clears counts and details", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 5})
		cos.Status.ObservedPhases[0].ObjectDetails = []ocv1.ObjectStatus{{Name: "stale"}}
		result := &fakeTeardownResult{
			complete: true,
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p1", complete: true},
			},
		}
		FromTeardown(&cos, result, now)
		require.Len(t, cos.Status.ObservedPhases, 1)
		phase := cos.Status.ObservedPhases[0]
		assert.Equal(t, ocv1.PhaseStatusTeardownComplete, phase.Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 5}, phase.ObjectCounts)
		assert.Empty(t, phase.ObjectDetails)
	})

	t.Run("objectDetails contain awaiting deletion with correct object identity", func(t *testing.T) {
		waitingObj := &unstructured.Unstructured{}
		waitingObj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
		waitingObj.SetName("my-cm")
		waitingObj.SetNamespace("default")
		ref := boxcuttertypes.ToObjectRef(waitingObj)

		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 1})
		result := &fakeTeardownResult{
			complete: false,
			active:   "p1",
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p1", complete: false, waiting: []boxcuttertypes.ObjectRef{ref}},
			},
		}
		FromTeardown(&cos, result, now)
		require.Len(t, cos.Status.ObservedPhases[0].ObjectDetails, 1)
		detail := cos.Status.ObservedPhases[0].ObjectDetails[0]
		assert.Equal(t, corev1.SchemeGroupVersion.Group, detail.Group)
		assert.Equal(t, corev1.SchemeGroupVersion.Version, detail.Version)
		assert.Equal(t, "ConfigMap", detail.Kind)
		assert.Equal(t, "default", detail.Namespace)
		assert.Equal(t, "my-cm", detail.Name)
		assert.Equal(t, []string{"awaiting deletion"}, detail.Messages)
	})

	t.Run("multi-phase teardown: status, message, details, and aggregate counts", func(t *testing.T) {
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
		require.Len(t, cos.Status.ObservedPhases, 3)

		phases := cos.Status.ObservedPhases

		// p1 — pending, waiting for p2 to finish first
		assert.Equal(t, ocv1.PhaseStatusPending, phases[0].Status)
		assert.Equal(t, "Waiting for later phases to complete teardown", phases[0].Message)
		assert.Equal(t, ocv1.ObjectCounts{Total: 2, Present: 2}, phases[0].ObjectCounts)
		assert.Len(t, phases[0].ObjectDetails, 2)

		// p2 — active teardown
		assert.Equal(t, ocv1.PhaseStatusTearingDown, phases[1].Status)
		assert.Empty(t, phases[1].Message)
		assert.Equal(t, ocv1.ObjectCounts{Total: 3, Present: 2}, phases[1].ObjectCounts)
		assert.Len(t, phases[1].ObjectDetails, 2)

		// p3 — complete, objectDetails cleared
		assert.Equal(t, ocv1.PhaseStatusTeardownComplete, phases[2].Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 4}, phases[2].ObjectCounts)
		assert.Empty(t, phases[2].ObjectDetails)

		// aggregate
		assert.Equal(t, ocv1.ObjectCounts{Total: 9, Present: 4}, cos.Status.ObjectCounts)
	})

	t.Run("spec phase absent from GetPhases gets Unknown status", func(t *testing.T) {
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
		// p2 has a result, p1 does not
		result := &fakeTeardownResult{
			complete: false,
			active:   "p2",
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p2", complete: false, waiting: []boxcuttertypes.ObjectRef{{}}},
			},
		}
		FromTeardown(&cos, result, now)
		require.Len(t, cos.Status.ObservedPhases, 2)
		assert.Equal(t, ocv1.PhaseStatusUnknown, cos.Status.ObservedPhases[0].Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 2}, cos.Status.ObservedPhases[0].ObjectCounts)
		assert.Empty(t, cos.Status.ObservedPhases[0].ObjectDetails)
	})

	t.Run("preserveCompletionTimes does not stamp CompletedAt on teardown phases", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 2})
		result := &fakeTeardownResult{
			complete: false,
			active:   "p1",
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p1", complete: false, waiting: []boxcuttertypes.ObjectRef{{}}},
			},
		}
		FromTeardown(&cos, result, now)
		assert.Nil(t, cos.Status.ObservedPhases[0].CompletedAt)
	})

	t.Run("CompletedAt from previous reconcile is preserved through teardown", func(t *testing.T) {
		earlier := metav1.NewTime(now.Add(-time.Hour))
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 2})
		cos.Status.ObservedPhases[0].CompletedAt = &earlier
		result := &fakeTeardownResult{
			complete: false,
			active:   "p1",
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p1", complete: false, waiting: []boxcuttertypes.ObjectRef{{}}},
			},
		}
		FromTeardown(&cos, result, now)
		require.NotNil(t, cos.Status.ObservedPhases[0].CompletedAt)
		assert.Equal(t, earlier, *cos.Status.ObservedPhases[0].CompletedAt)
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

	t.Run("sets completedAt on Ready phase with no completedAt", func(t *testing.T) {
		phases := []ocv1.ObservedPhase{
			{Name: "phase-1", Status: ocv1.PhaseStatusReady},
		}
		preserveCompletionTimes(&phases, now)
		require.NotNil(t, phases[0].CompletedAt)
		assert.Equal(t, now, *phases[0].CompletedAt)
	})

	t.Run("does not set completedAt on non-Ready phase", func(t *testing.T) {
		phases := []ocv1.ObservedPhase{
			{Name: "phase-1", Status: ocv1.PhaseStatusReconciling},
		}
		preserveCompletionTimes(&phases, now)
		assert.Nil(t, phases[0].CompletedAt)
	})

	t.Run("does not overwrite existing completedAt on Ready phase", func(t *testing.T) {
		phases := []ocv1.ObservedPhase{
			{Name: "phase-1", Status: ocv1.PhaseStatusReady, CompletedAt: &earlier},
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
		assert.Equal(t, ocv1.PhaseStatusReady, observed[0].Status)
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

	t.Run("write actions (Created, Updated, Recovered) do not count as Synced", func(t *testing.T) {
		// Only ActionIdle increments Synced; write actions must not, otherwise
		// WaitingForAssertions fires on the same cycle as the write.
		results := []machinery.PhaseResult{
			&fakePhaseResult{
				name:     "phase-1",
				complete: false,
				objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionCreated},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionUpdated},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionRecovered},
				},
			},
		}
		// spec has 2 objects for phase-1 but we provide 3 result objects to cover all write actions
		customSpec := cosSpecWithPhaseObjects(struct {
			name    string
			objects int
		}{"phase-1", 3})
		var observed []ocv1.ObservedPhase
		buildObservedPhases(customSpec.Phases, &observed, results)
		assert.Equal(t, ocv1.PhaseStatusReconciling, observed[0].Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 3, Present: 3, Synced: 0}, observed[0].ObjectCounts)
	})

	t.Run("mix of idle and written objects reports Reconciling until all are idle", func(t *testing.T) {
		results := []machinery.PhaseResult{
			&fakePhaseResult{
				name:     "phase-1",
				complete: false,
				objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionUpdated},
				},
			},
		}
		var observed []ocv1.ObservedPhase
		buildObservedPhases(spec.Phases, &observed, results)
		assert.Equal(t, ocv1.PhaseStatusReconciling, observed[0].Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 2, Present: 2, Synced: 1}, observed[0].ObjectCounts)
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
		assert.Len(t, observed[1].ObjectDetails, 2)
	})
}

func TestObjectMessages(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	obj.SetName("my-cm")

	t.Run("collision returns ownership collision message", func(t *testing.T) {
		msgs := objectMessages(&fakeCollisionResult{
			fakeObjectResult: fakeObjectResult{obj: obj, probes: boxcuttertypes.ProbeResultContainer{}},
		})
		assert.Equal(t, []string{"object ownership collision"}, msgs)
	})

	t.Run("failing probe with message", func(t *testing.T) {
		msgs := objectMessages(&fakeObjectResult{
			obj: obj,
			probes: boxcuttertypes.ProbeResultContainer{
				"readiness": {Status: boxcuttertypes.ProbeStatusFalse, Messages: []string{"not ready"}},
			},
		})
		assert.Equal(t, []string{"readiness: not ready"}, msgs)
	})

	t.Run("failing probe without message shows status", func(t *testing.T) {
		msgs := objectMessages(&fakeObjectResult{
			obj: obj,
			probes: boxcuttertypes.ProbeResultContainer{
				"readiness": {Status: boxcuttertypes.ProbeStatusFalse},
			},
		})
		assert.Equal(t, []string{"readiness: False"}, msgs)
	})

	t.Run("passing probe is omitted", func(t *testing.T) {
		msgs := objectMessages(&fakeObjectResult{
			obj: obj,
			probes: boxcuttertypes.ProbeResultContainer{
				"readiness": {Status: boxcuttertypes.ProbeStatusTrue},
			},
		})
		assert.Equal(t, []string{"not yet complete"}, msgs)
	})

	t.Run("paused non-idle delegates to pausedObjectMessages", func(t *testing.T) {
		msgs := objectMessages(&fakeObjectResult{
			obj:    obj,
			paused: true,
			action: machinery.ActionCreated,
			probes: boxcuttertypes.ProbeResultContainer{},
		})
		assert.Equal(t, []string{"object does not exist"}, msgs)
	})

	t.Run("no issues returns not yet complete", func(t *testing.T) {
		msgs := objectMessages(&fakeObjectResult{
			obj:    obj,
			action: machinery.ActionIdle,
			probes: boxcuttertypes.ProbeResultContainer{},
		})
		assert.Equal(t, []string{"not yet complete"}, msgs)
	})
}

type fakeCompareObjectResult struct {
	fakeObjectResult
	compareResult machinery.CompareResult
}

func (f *fakeCompareObjectResult) CompareResult() machinery.CompareResult { return f.compareResult }

func TestPausedObjectMessages(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	obj.SetName("my-cm")

	t.Run("ActionCreated returns object does not exist", func(t *testing.T) {
		msgs := pausedObjectMessages(&fakeObjectResult{obj: obj, paused: true, action: machinery.ActionCreated})
		assert.Equal(t, []string{"object does not exist"}, msgs)
	})

	t.Run("ActionRecovered without compare result", func(t *testing.T) {
		msgs := pausedObjectMessages(&fakeObjectResult{obj: obj, paused: true, action: machinery.ActionRecovered})
		assert.Equal(t, []string{"object was modified by another actor"}, msgs)
	})

	t.Run("ActionRecovered with compare result", func(t *testing.T) {
		msgs := pausedObjectMessages(&fakeCompareObjectResult{
			fakeObjectResult: fakeObjectResult{obj: obj, paused: true, action: machinery.ActionRecovered},
			compareResult: machinery.CompareResult{
				Comparison: &typed.Comparison{
					Modified: fieldpath.NewSet(fieldpath.MakePathOrDie("data", "key")),
				},
			},
		})
		require.Len(t, msgs, 1)
		assert.Contains(t, msgs[0], "object was modified by another actor")
		assert.Contains(t, msgs[0], " - modified: .data.key")
	})

	t.Run("default action without compare result", func(t *testing.T) {
		msgs := pausedObjectMessages(&fakeObjectResult{obj: obj, paused: true, action: machinery.ActionUpdated})
		assert.Equal(t, []string{"object content has changed"}, msgs)
	})

	t.Run("default action with compare result", func(t *testing.T) {
		msgs := pausedObjectMessages(&fakeCompareObjectResult{
			fakeObjectResult: fakeObjectResult{obj: obj, paused: true, action: machinery.ActionUpdated},
			compareResult: machinery.CompareResult{
				Comparison: &typed.Comparison{
					Added: fieldpath.NewSet(fieldpath.MakePathOrDie("data", "new-key")),
				},
			},
		})
		require.Len(t, msgs, 1)
		assert.Contains(t, msgs[0], "object content has changed")
		assert.Contains(t, msgs[0], " - added: .data.new-key")
	})

	t.Run("long summary is truncated to maxMessageLength", func(t *testing.T) {
		modified := fieldpath.NewSet()
		for i := range 200 {
			modified.Insert(fieldpath.MakePathOrDie("data", strings.Repeat("k", i+1)))
		}
		msgs := pausedObjectMessages(&fakeCompareObjectResult{
			fakeObjectResult: fakeObjectResult{obj: obj, paused: true, action: machinery.ActionRecovered},
			compareResult: machinery.CompareResult{
				Comparison: &typed.Comparison{Modified: modified},
			},
		})
		require.Len(t, msgs, 1)
		assert.LessOrEqual(t, len([]rune(msgs[0])), maxMessageLength)
	})
}

func TestCompareDetails(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	obj.SetName("my-cm")

	t.Run("object without compareResulter returns nil", func(t *testing.T) {
		result := compareDetails(&fakeObjectResult{obj: obj})
		assert.Nil(t, result)
	})

	t.Run("nil Comparison returns nil", func(t *testing.T) {
		result := compareDetails(&fakeCompareObjectResult{
			fakeObjectResult: fakeObjectResult{obj: obj},
			compareResult:    machinery.CompareResult{Comparison: nil},
		})
		assert.Nil(t, result)
	})

	t.Run("added fields only", func(t *testing.T) {
		result := compareDetails(&fakeCompareObjectResult{
			fakeObjectResult: fakeObjectResult{obj: obj},
			compareResult: machinery.CompareResult{
				Comparison: &typed.Comparison{
					Added: fieldpath.NewSet(fieldpath.MakePathOrDie("data", "foo")),
				},
			},
		})
		assert.Equal(t, []string{" - added: .data.foo"}, result)
	})

	t.Run("modified fields only", func(t *testing.T) {
		result := compareDetails(&fakeCompareObjectResult{
			fakeObjectResult: fakeObjectResult{obj: obj},
			compareResult: machinery.CompareResult{
				Comparison: &typed.Comparison{
					Modified: fieldpath.NewSet(fieldpath.MakePathOrDie("data", "bar")),
				},
			},
		})
		assert.Equal(t, []string{" - modified: .data.bar"}, result)
	})

	t.Run("removed fields only", func(t *testing.T) {
		result := compareDetails(&fakeCompareObjectResult{
			fakeObjectResult: fakeObjectResult{obj: obj},
			compareResult: machinery.CompareResult{
				Comparison: &typed.Comparison{
					Removed: fieldpath.NewSet(fieldpath.MakePathOrDie("data", "old")),
				},
			},
		})
		assert.Equal(t, []string{" - removed: .data.old"}, result)
	})

	t.Run("all three field types", func(t *testing.T) {
		result := compareDetails(&fakeCompareObjectResult{
			fakeObjectResult: fakeObjectResult{obj: obj},
			compareResult: machinery.CompareResult{
				Comparison: &typed.Comparison{
					Added:    fieldpath.NewSet(fieldpath.MakePathOrDie("data", "new")),
					Modified: fieldpath.NewSet(fieldpath.MakePathOrDie("data", "changed")),
					Removed:  fieldpath.NewSet(fieldpath.MakePathOrDie("data", "gone")),
				},
			},
		})
		require.Len(t, result, 3)
		assert.Equal(t, " - added: .data.new", result[0])
		assert.Equal(t, " - modified: .data.changed", result[1])
		assert.Equal(t, " - removed: .data.gone", result[2])
	})
}
