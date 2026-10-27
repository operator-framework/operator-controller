package status

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"pkg.package-operator.run/boxcutter/machinery"
	boxcuttertypes "pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/validation"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v6/typed"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestUpdateObservedPhasesForReconcile(t *testing.T) {
	now := metav1.Now()

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	obj.SetName("my-cm")

	t.Run("revision validation error skips all phase updates", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 1})
		cos.Status.ObservedPhases[0].Status = ocv1.PhaseStatusReconciling
		result := &fakeRevisionResult{
			validationError: &validation.RevisionValidationError{},
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: true},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		// phases must not be updated when there is a revision-level validation error
		assert.Equal(t, ocv1.PhaseStatusReconciling, cos.Status.ObservedPhases[0].Status)
	})

	t.Run("phase validation error when preceding phases are complete reports InvalidSpecification", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 1})
		ve := &validation.PhaseValidationError{}
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: true, validationError: ve},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		assert.Equal(t, ocv1.PhaseStatusInvalidSpecification, cos.Status.ObservedPhases[0].Status)
		assert.Equal(t, int64(1), cos.Status.ObservedPhases[0].ObjectCounts.Total)
	})

	t.Run("forbidden dry-run response reports PreconditionsNotMet not InvalidSpecification", func(t *testing.T) {
		// A Forbidden response means RBAC has not yet propagated; the controller retries
		// automatically so this must not be reported as a permanent InvalidSpecification.
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 1})
		forbiddenErr := apierrors.NewForbidden(
			schema.GroupResource{Group: "apps", Resource: "deployments"},
			"my-deploy",
			fmt.Errorf("not allowed"),
		)
		ve := &validation.PhaseValidationError{
			PhaseName: "p1",
			Objects: []validation.ObjectValidationError{
				{
					ObjectRef: boxcuttertypes.ToObjectRef(obj),
					Errors:    []error{forbiddenErr},
				},
			},
		}
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: false, validationError: ve},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		assert.Equal(t, ocv1.PhaseStatusPreconditionsNotMet, cos.Status.ObservedPhases[0].Status)
	})

	t.Run("permanent dry-run response (Invalid) reports InvalidSpecification not PreconditionsNotMet", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 1})
		invalidErr := apierrors.NewInvalid(
			schema.GroupKind{Group: "apps", Kind: "Deployment"},
			"my-deploy",
			nil,
		)
		ve := &validation.PhaseValidationError{
			PhaseName: "p1",
			Objects: []validation.ObjectValidationError{
				{
					ObjectRef: boxcuttertypes.ToObjectRef(obj),
					Errors:    []error{invalidErr},
				},
			},
		}
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: false, validationError: ve},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		assert.Equal(t, ocv1.PhaseStatusInvalidSpecification, cos.Status.ObservedPhases[0].Status)
	})

	t.Run("phase validation error when preceding phases are incomplete reports Pending", func(t *testing.T) {
		cos := cosWithPhaseObjects(
			struct {
				name    string
				objects int
			}{"p1", 1},
			struct {
				name    string
				objects int
			}{"p2", 1},
		)
		ve := &validation.PhaseValidationError{}
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				// p1 is incomplete — this sets precedingPhasesComplete = false
				&fakePhaseResult{name: "p1", complete: false, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionIdle},
				}},
				// p2 has a validation error but preceding phase is not complete
				&fakePhaseResult{name: "p2", complete: false, validationError: ve},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		assert.Equal(t, ocv1.PhaseStatusPending, cos.Status.ObservedPhases[1].Status)
		assert.Equal(t, int64(1), cos.Status.ObservedPhases[1].ObjectCounts.Total)
	})

	t.Run("per-phase progressed marks phase as Superseded", func(t *testing.T) {
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
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: false, progressed: true, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionProgressed},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionProgressed},
				}},
				&fakePhaseResult{name: "p2", complete: false, progressed: true, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionProgressed},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionProgressed},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionProgressed},
				}},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		require.Len(t, cos.Status.ObservedPhases, 2)
		assert.Equal(t, ocv1.PhaseStatusSuperseded, cos.Status.ObservedPhases[0].Status)
		assert.Equal(t, int64(2), cos.Status.ObservedPhases[0].ObjectCounts.Total)
		assert.Equal(t, ocv1.PhaseStatusSuperseded, cos.Status.ObservedPhases[1].Status)
		assert.Equal(t, int64(3), cos.Status.ObservedPhases[1].ObjectCounts.Total)
	})

	t.Run("empty phase with HasProgressed vacuously true does not report Superseded", func(t *testing.T) {
		// boxcutter's HasProgressed() returns true vacuously for a 0-object phase (0 == 0).
		// The len > 0 guard must prevent this from being reported as Superseded.
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 0})
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: true, progressed: true},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		assert.Equal(t, ocv1.PhaseStatusReady, cos.Status.ObservedPhases[0].Status)
	})

	t.Run("complete phase with any ActionProgressed object reports Superseded not Ready", func(t *testing.T) {
		// HasProgressed()==false but the phase is complete and one object has been claimed
		// by a newer revision. The phase must show Superseded, not Ready, so the caller
		// does not treat this revision as the active installed one.
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 2})
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: true, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: true, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionProgressed},
				}},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		assert.Equal(t, ocv1.PhaseStatusSuperseded, cos.Status.ObservedPhases[0].Status)
	})

	t.Run("collision object is not counted as Present and phase reports Reconciling", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 1})
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: false, objects: []machinery.ObjectResult{
					&fakeCollisionResult{
						fakeObjectResult: fakeObjectResult{obj: obj, complete: false, probes: boxcuttertypes.ProbeResultContainer{}},
					},
				}},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		require.NotNil(t, cos.Status.ObservedPhases)
		assert.Equal(t, ocv1.ObjectCounts{Total: 1}, cos.Status.ObservedPhases[0].ObjectCounts)
		assert.Equal(t, ocv1.PhaseStatusReconciling, cos.Status.ObservedPhases[0].Status)
	})

	t.Run("complete phase reports Ready with full counts", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 2})
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: true, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: true, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: true, action: machinery.ActionIdle},
				}},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		assert.Equal(t, ocv1.PhaseStatusReady, cos.Status.ObservedPhases[0].Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 2, Present: 2, Synced: 2, Ready: 2}, cos.Status.ObservedPhases[0].ObjectCounts)
	})

	t.Run("all idle objects reports Stabilizing", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 2})
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: false, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: true, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionIdle},
				}},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		assert.Equal(t, ocv1.PhaseStatusStabilizing, cos.Status.ObservedPhases[0].Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 2, Present: 2, Synced: 2, Ready: 1}, cos.Status.ObservedPhases[0].ObjectCounts)
	})

	t.Run("write actions (Created, Updated, Recovered) count as Synced for non-paused objects", func(t *testing.T) {
		// All three write actions increment Synced for non-paused objects,
		// so a freshly-applied phase immediately transitions to Stabilizing.
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 3})
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: false, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionCreated},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionUpdated},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionRecovered},
				}},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		assert.Equal(t, ocv1.PhaseStatusStabilizing, cos.Status.ObservedPhases[0].Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 3, Present: 3, Synced: 3}, cos.Status.ObservedPhases[0].ObjectCounts)
	})

	t.Run("all-paused phase reports Pending with write actions not counted as Synced", func(t *testing.T) {
		// ActionUpdated and ActionRecovered on paused objects do not count as Synced;
		// paused ActionCreated counts as neither Present nor Synced.
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 3})
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: false, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: false, paused: true, action: machinery.ActionUpdated},
					&fakeObjectResult{obj: obj, complete: false, paused: true, action: machinery.ActionRecovered},
					&fakeObjectResult{obj: obj, complete: false, paused: true, action: machinery.ActionCreated},
				}},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		// isPaused=true → default case → Pending (read-only while earlier phase is rolling out)
		assert.Equal(t, ocv1.PhaseStatusPending, cos.Status.ObservedPhases[0].Status)
		// ActionUpdated+paused: Present; ActionRecovered+paused: Present; ActionCreated+paused: nothing
		assert.Equal(t, ocv1.ObjectCounts{Total: 3, Present: 2, Synced: 0}, cos.Status.ObservedPhases[0].ObjectCounts)
	})

	t.Run("phase with paused objects from blocked preceding phase reports Pending", func(t *testing.T) {
		cos := cosWithPhaseObjects(
			struct {
				name    string
				objects int
			}{"phase-1", 2},
			struct {
				name    string
				objects int
			}{"phase-2", 3},
		)
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "phase-1", complete: true, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: true, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: true, action: machinery.ActionIdle},
				}},
				&fakePhaseResult{name: "phase-2", complete: false, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: true, paused: true, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: false, paused: true, action: machinery.ActionUpdated},
					&fakeObjectResult{obj: obj, complete: false, paused: true, action: machinery.ActionCreated},
				}},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		require.Len(t, cos.Status.ObservedPhases, 2)
		assert.Equal(t, ocv1.PhaseStatusPending, cos.Status.ObservedPhases[1].Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 3, Present: 2, Synced: 1, Ready: 1}, cos.Status.ObservedPhases[1].ObjectCounts)
		assert.Len(t, cos.Status.ObservedPhases[1].ObjectDetails, 2)
	})

	t.Run("Ready phase has no objectDetails", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 2})
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: true, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: true, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: true, action: machinery.ActionIdle},
				}},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		assert.Equal(t, ocv1.PhaseStatusReady, cos.Status.ObservedPhases[0].Status)
		assert.Empty(t, cos.Status.ObservedPhases[0].ObjectDetails)
	})

	t.Run("Superseded phase has no objectDetails", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 2})
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: false, progressed: true, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionProgressed},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionProgressed},
				}},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		assert.Equal(t, ocv1.PhaseStatusSuperseded, cos.Status.ObservedPhases[0].Status)
		assert.Empty(t, cos.Status.ObservedPhases[0].ObjectDetails)
	})

	t.Run("Stabilizing phase populates objectDetails", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 2})
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: false, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: true, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionIdle},
				}},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		assert.Equal(t, ocv1.PhaseStatusStabilizing, cos.Status.ObservedPhases[0].Status)
		assert.Len(t, cos.Status.ObservedPhases[0].ObjectDetails, 1)
	})

	t.Run("Reconciling phase populates objectDetails", func(t *testing.T) {
		// ActionProgressed counts as Present but not Synced, so isSynced=false and isPaused=false → Reconciling.
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 2})
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: false, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionProgressed},
				}},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		assert.Equal(t, ocv1.PhaseStatusReconciling, cos.Status.ObservedPhases[0].Status)
		assert.Len(t, cos.Status.ObservedPhases[0].ObjectDetails, 1)
	})

	t.Run("ActionProgressed objects are excluded from objectDetails", func(t *testing.T) {
		// ActionProgressed means the object has been claimed by a newer revision;
		// it is not a failure condition and must not appear in objectDetails.
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 2})
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: false, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionProgressed},
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionCreated},
				}},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		require.Len(t, cos.Status.ObservedPhases[0].ObjectDetails, 1)
		assert.Equal(t, "my-cm", cos.Status.ObservedPhases[0].ObjectDetails[0].Name)
	})

	t.Run("aggregate ObjectCounts sums across phases", func(t *testing.T) {
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
		result := &fakeRevisionResult{
			phases: []machinery.PhaseResult{
				&fakePhaseResult{name: "p1", complete: true, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: true, action: machinery.ActionIdle},
					&fakeObjectResult{obj: obj, complete: true, action: machinery.ActionIdle},
				}},
				&fakePhaseResult{name: "p2", complete: false, objects: []machinery.ObjectResult{
					&fakeObjectResult{obj: obj, complete: false, action: machinery.ActionIdle},
				}},
			},
		}
		UpdateObservedPhasesForReconcile(&cos, result, now)
		// p1: Total=2,Present=2,Synced=2,Ready=2 — p2: Total=3,Present=1,Synced=1,Ready=0
		assert.Equal(t, ocv1.ObjectCounts{Total: 5, Present: 3, Synced: 3, Ready: 2}, cos.Status.ObjectCounts)
	})
}

func TestUpdateObservedPhasesForTeardown(t *testing.T) {
	now := metav1.Now()

	t.Run("active phase gets Archiving status with message", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 3})
		result := &fakeTeardownResult{
			active: "p1",
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p1", waiting: []boxcuttertypes.ObjectRef{{}, {}}},
			},
		}
		UpdateObservedPhasesForTeardown(&cos, result, now)
		require.Len(t, cos.Status.ObservedPhases, 1)
		phase := cos.Status.ObservedPhases[0]
		assert.Equal(t, ocv1.PhaseStatusArchiving, phase.Status)
		assert.Equal(t, "removing all phase objects owned by this object set", phase.Message)
		assert.Equal(t, ocv1.ObjectCounts{Total: 3, Present: 2}, phase.ObjectCounts)
		assert.Len(t, phase.ObjectDetails, 2)
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
			active: "p1",
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p1", waiting: []boxcuttertypes.ObjectRef{ref}},
			},
		}
		UpdateObservedPhasesForTeardown(&cos, result, now)
		require.Len(t, cos.Status.ObservedPhases[0].ObjectDetails, 1)
		detail := cos.Status.ObservedPhases[0].ObjectDetails[0]
		assert.Equal(t, corev1.SchemeGroupVersion.Group, detail.Group)
		assert.Equal(t, corev1.SchemeGroupVersion.Version, detail.Version)
		assert.Equal(t, "ConfigMap", detail.Kind)
		assert.Equal(t, "default", detail.Namespace)
		assert.Equal(t, "my-cm", detail.Name)
		assert.Equal(t, []string{"awaiting deletion"}, detail.Messages)
	})

	t.Run("gone phase gets Archived status with message", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 5})
		result := &fakeTeardownResult{
			gone: []string{"p1"},
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p1"},
			},
		}
		UpdateObservedPhasesForTeardown(&cos, result, now)
		require.Len(t, cos.Status.ObservedPhases, 1)
		phase := cos.Status.ObservedPhases[0]
		assert.Equal(t, ocv1.PhaseStatusArchived, phase.Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 5}, phase.ObjectCounts)
		assert.Empty(t, phase.ObjectDetails)
	})

	t.Run("multi-phase teardown: status, messages, and aggregate counts", func(t *testing.T) {
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
			active: "p2",
			gone:   []string{"p3"},
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p3"},
				&fakePhaseTeardownResult{name: "p2", waiting: []boxcuttertypes.ObjectRef{{}, {}}},
				&fakePhaseTeardownResult{name: "p1", waiting: []boxcuttertypes.ObjectRef{{}, {}}},
			},
		}
		UpdateObservedPhasesForTeardown(&cos, result, now)
		require.Len(t, cos.Status.ObservedPhases, 3)

		phases := cos.Status.ObservedPhases

		// p1 — pending, waiting for later phases to complete
		assert.Equal(t, ocv1.PhaseStatusPending, phases[0].Status)
		assert.Equal(t, "waiting for later phases to complete teardown", phases[0].Message)
		assert.Equal(t, ocv1.ObjectCounts{Total: 2, Present: 2}, phases[0].ObjectCounts)
		assert.Len(t, phases[0].ObjectDetails, 2)

		// p2 — active teardown
		assert.Equal(t, ocv1.PhaseStatusArchiving, phases[1].Status)
		assert.Equal(t, "removing all phase objects owned by this object set", phases[1].Message)
		assert.Equal(t, ocv1.ObjectCounts{Total: 3, Present: 2}, phases[1].ObjectCounts)
		assert.Len(t, phases[1].ObjectDetails, 2)

		// p3 — complete (in GonePhaseNames), no waiting objects
		assert.Equal(t, ocv1.PhaseStatusArchived, phases[2].Status)
		assert.Equal(t, ocv1.ObjectCounts{Total: 4}, phases[2].ObjectCounts)
		assert.Empty(t, phases[2].ObjectDetails)

		// aggregate
		assert.Equal(t, ocv1.ObjectCounts{Total: 9, Present: 4}, cos.Status.ObjectCounts)
	})

	t.Run("phase absent from GetPhases retains its previous status", func(t *testing.T) {
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
		// Set a recognisable prior status on p1
		cos.Status.ObservedPhases[0].Status = ocv1.PhaseStatusReconciling
		// Only p2 has a result; p1 is absent from GetPhases
		result := &fakeTeardownResult{
			active: "p2",
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p2", waiting: []boxcuttertypes.ObjectRef{{}}},
			},
		}
		UpdateObservedPhasesForTeardown(&cos, result, now)
		require.Len(t, cos.Status.ObservedPhases, 2)
		// p1 not in GetPhases — must not be touched
		assert.Equal(t, ocv1.PhaseStatusReconciling, cos.Status.ObservedPhases[0].Status)
	})

	t.Run("CompletedAt is not stamped on non-Ready teardown phases", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 2})
		result := &fakeTeardownResult{
			active: "p1",
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p1", waiting: []boxcuttertypes.ObjectRef{{}}},
			},
		}
		UpdateObservedPhasesForTeardown(&cos, result, now)
		assert.True(t, cos.Status.ObservedPhases[0].CompletedAt.IsZero())
	})

	t.Run("CompletedAt from previous reconcile is preserved through teardown", func(t *testing.T) {
		earlier := metav1.NewTime(now.Add(-time.Hour))
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 2})
		cos.Status.ObservedPhases[0].CompletedAt = earlier
		result := &fakeTeardownResult{
			active: "p1",
			phases: []machinery.PhaseTeardownResult{
				&fakePhaseTeardownResult{name: "p1", waiting: []boxcuttertypes.ObjectRef{{}}},
			},
		}
		UpdateObservedPhasesForTeardown(&cos, result, now)
		require.NotNil(t, cos.Status.ObservedPhases[0].CompletedAt)
		assert.Equal(t, earlier, cos.Status.ObservedPhases[0].CompletedAt)
	})
}

func TestUpdateObservedPhaseStatus(t *testing.T) {
	fixedNow := metav1.NewTime(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))
	earlier := metav1.NewTime(fixedNow.Add(-time.Hour))

	t.Run("sets CompletedAt on first Ready status", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 1})
		updateObservedPhaseStatus(&cos, ocv1.ObservedPhase{Name: "p1", Status: ocv1.PhaseStatusReady}, fixedNow)
		require.NotNil(t, cos.Status.ObservedPhases[0].CompletedAt)
		assert.Equal(t, fixedNow, cos.Status.ObservedPhases[0].CompletedAt)
	})

	t.Run("does not set CompletedAt on non-Ready status", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 1})
		updateObservedPhaseStatus(&cos, ocv1.ObservedPhase{Name: "p1", Status: ocv1.PhaseStatusReconciling}, fixedNow)
		assert.True(t, cos.Status.ObservedPhases[0].CompletedAt.IsZero())
	})

	t.Run("preserves existing CompletedAt regardless of current status", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 1})
		cos.Status.ObservedPhases[0].CompletedAt = earlier
		updateObservedPhaseStatus(&cos, ocv1.ObservedPhase{Name: "p1", Status: ocv1.PhaseStatusArchiving}, fixedNow)
		require.NotNil(t, cos.Status.ObservedPhases[0].CompletedAt)
		assert.Equal(t, earlier, cos.Status.ObservedPhases[0].CompletedAt)
	})

	t.Run("advances LastTransitionTime on status change", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 1})
		cos.Status.ObservedPhases[0].Status = ocv1.PhaseStatusReconciling
		cos.Status.ObservedPhases[0].LastTransitionTime = earlier
		updateObservedPhaseStatus(&cos, ocv1.ObservedPhase{Name: "p1", Status: ocv1.PhaseStatusStabilizing}, fixedNow)
		assert.Equal(t, fixedNow, cos.Status.ObservedPhases[0].LastTransitionTime)
	})

	t.Run("preserves LastTransitionTime when status is unchanged", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 1})
		cos.Status.ObservedPhases[0].Status = ocv1.PhaseStatusReconciling
		cos.Status.ObservedPhases[0].LastTransitionTime = earlier
		updateObservedPhaseStatus(&cos, ocv1.ObservedPhase{Name: "p1", Status: ocv1.PhaseStatusReconciling}, fixedNow)
		assert.Equal(t, earlier, cos.Status.ObservedPhases[0].LastTransitionTime)
	})

	t.Run("preserves Digest from previous observation", func(t *testing.T) {
		cos := cosWithPhaseObjects(struct {
			name    string
			objects int
		}{"p1", 1})
		cos.Status.ObservedPhases[0].Digest = "sha256:abc123"
		updateObservedPhaseStatus(&cos, ocv1.ObservedPhase{Name: "p1", Status: ocv1.PhaseStatusReconciling}, fixedNow)
		assert.Equal(t, "sha256:abc123", cos.Status.ObservedPhases[0].Digest)
	})

	t.Run("appends new phase when not yet in ObservedPhases", func(t *testing.T) {
		// Start with a COS that has no pre-existing ObservedPhases
		cos := ocv1.ClusterObjectSet{
			Spec: cosSpecWithPhaseObjects(struct {
				name    string
				objects int
			}{"p1", 1}),
		}
		updateObservedPhaseStatus(&cos, ocv1.ObservedPhase{Name: "p1", Status: ocv1.PhaseStatusReconciling}, fixedNow)
		require.Len(t, cos.Status.ObservedPhases, 1)
		assert.Equal(t, "p1", cos.Status.ObservedPhases[0].Name)
		assert.Equal(t, ocv1.PhaseStatusReconciling, cos.Status.ObservedPhases[0].Status)
		assert.Equal(t, fixedNow, cos.Status.ObservedPhases[0].LastTransitionTime)
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

func TestObjectStatus(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	obj.SetName("my-cm")
	obj.SetNamespace("default")

	t.Run("populates object identity from GVK and object ref", func(t *testing.T) {
		result := objectStatus(&fakeObjectResult{obj: obj, probes: boxcuttertypes.ProbeResultContainer{}})
		assert.Equal(t, corev1.SchemeGroupVersion.Group, result.Group)
		assert.Equal(t, corev1.SchemeGroupVersion.Version, result.Version)
		assert.Equal(t, "ConfigMap", result.Kind)
		assert.Equal(t, "default", result.Namespace)
		assert.Equal(t, "my-cm", result.Name)
	})

	t.Run("collision returns ownership collision message", func(t *testing.T) {
		result := objectStatus(&fakeCollisionResult{
			fakeObjectResult: fakeObjectResult{obj: obj, probes: boxcuttertypes.ProbeResultContainer{}},
		})
		assert.Equal(t, []string{"object ownership collision"}, result.Messages)
	})

	t.Run("failing probe with message", func(t *testing.T) {
		result := objectStatus(&fakeObjectResult{
			obj: obj,
			probes: boxcuttertypes.ProbeResultContainer{
				"readiness": {Status: boxcuttertypes.ProbeStatusFalse, Messages: []string{"not ready"}},
			},
		})
		assert.Equal(t, []string{"readiness: not ready"}, result.Messages)
	})

	t.Run("failing probe without message shows status", func(t *testing.T) {
		result := objectStatus(&fakeObjectResult{
			obj: obj,
			probes: boxcuttertypes.ProbeResultContainer{
				"readiness": {Status: boxcuttertypes.ProbeStatusFalse},
			},
		})
		assert.Equal(t, []string{"readiness: False"}, result.Messages)
	})

	t.Run("passing probe is omitted and falls back to not yet complete", func(t *testing.T) {
		result := objectStatus(&fakeObjectResult{
			obj: obj,
			probes: boxcuttertypes.ProbeResultContainer{
				"readiness": {Status: boxcuttertypes.ProbeStatusTrue},
			},
		})
		assert.Equal(t, []string{"not yet complete"}, result.Messages)
	})

	t.Run("paused non-idle delegates to pausedObjectMessages", func(t *testing.T) {
		result := objectStatus(&fakeObjectResult{
			obj:    obj,
			paused: true,
			action: machinery.ActionCreated,
			probes: boxcuttertypes.ProbeResultContainer{},
		})
		assert.Equal(t, []string{"object does not exist"}, result.Messages)
	})

	t.Run("no issues returns not yet complete", func(t *testing.T) {
		result := objectStatus(&fakeObjectResult{
			obj:    obj,
			action: machinery.ActionIdle,
			probes: boxcuttertypes.ProbeResultContainer{},
		})
		assert.Equal(t, []string{"not yet complete"}, result.Messages)
	})

	t.Run("more than 18 messages are cut to 17 plus a truncation notice", func(t *testing.T) {
		probes := boxcuttertypes.ProbeResultContainer{}
		for i := range 20 {
			probes[fmt.Sprintf("probe-%02d", i)] = boxcuttertypes.ProbeResult{
				Status:   boxcuttertypes.ProbeStatusFalse,
				Messages: []string{"failed"},
			}
		}
		result := objectStatus(&fakeObjectResult{obj: obj, probes: probes})
		require.Len(t, result.Messages, 18)
		assert.Equal(t, "additional objectDetails messages were removed for size constraints", result.Messages[17])
	})
}

func TestPausedObjectMessages(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	obj.SetName("my-cm")

	t.Run("ActionCreated returns object does not exist", func(t *testing.T) {
		msgs := pausedObjectMessages(&fakeObjectResult{obj: obj, paused: true, action: machinery.ActionCreated})
		assert.Equal(t, []string{"object does not exist"}, msgs)
	})

	t.Run("ActionCollision without compare result returns ownership collision", func(t *testing.T) {
		msgs := pausedObjectMessages(&fakeObjectResult{obj: obj, paused: true, action: machinery.ActionCollision})
		assert.Equal(t, []string{"object ownership collision"}, msgs)
	})

	t.Run("ActionCollision with compare result includes diff details", func(t *testing.T) {
		msgs := pausedObjectMessages(&fakeCompareObjectResult{
			fakeObjectResult: fakeObjectResult{obj: obj, paused: true, action: machinery.ActionCollision},
			compareResult: machinery.CompareResult{
				Comparison: &typed.Comparison{
					Modified: fieldpath.NewSet(fieldpath.MakePathOrDie("metadata", "annotations")),
				},
			},
		})
		require.Len(t, msgs, 1)
		assert.Contains(t, msgs[0], "object ownership collision")
		assert.Contains(t, msgs[0], " - modified: .metadata.annotations")
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
	progressed      bool
	objects         []machinery.ObjectResult
	validationError *validation.PhaseValidationError
}

func (f *fakePhaseResult) GetName() string                      { return f.name }
func (f *fakePhaseResult) IsComplete() bool                     { return f.complete }
func (f *fakePhaseResult) HasProgressed() bool                  { return f.progressed }
func (f *fakePhaseResult) GetObjects() []machinery.ObjectResult { return f.objects }
func (f *fakePhaseResult) InTransition() bool                   { return false }
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

type fakeCompareObjectResult struct {
	fakeObjectResult
	compareResult machinery.CompareResult
}

func (f *fakeCompareObjectResult) CompareResult() machinery.CompareResult { return f.compareResult }

type fakeTeardownResult struct {
	complete bool
	active   string
	gone     []string
	phases   []machinery.PhaseTeardownResult
	waiting  []string
}

func (f *fakeTeardownResult) IsComplete() bool                           { return f.complete }
func (f *fakeTeardownResult) GetPhases() []machinery.PhaseTeardownResult { return f.phases }
func (f *fakeTeardownResult) GetActivePhaseName() (string, bool)         { return f.active, f.active != "" }
func (f *fakeTeardownResult) GetWaitingPhaseNames() []string             { return f.waiting }
func (f *fakeTeardownResult) GetGonePhaseNames() []string                { return f.gone }
func (f *fakeTeardownResult) String() string                             { return "" }

type fakePhaseTeardownResult struct {
	name    string
	waiting []boxcuttertypes.ObjectRef
}

func (f *fakePhaseTeardownResult) GetName() string                     { return f.name }
func (f *fakePhaseTeardownResult) IsComplete() bool                    { return len(f.waiting) == 0 }
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
