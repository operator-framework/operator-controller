package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"pkg.package-operator.run/boxcutter/machinery"
	machinerytypes "pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/validation"
)

// Mock implementations for testing
type mockRevisionResult struct {
	validationError *validation.RevisionValidationError
	phases          []machinery.PhaseResult
	inTransition    bool
	isComplete      bool
	hasProgressed   bool
}

func (m mockRevisionResult) GetValidationError() *validation.RevisionValidationError {
	return m.validationError
}

func (m mockRevisionResult) GetPhases() []machinery.PhaseResult {
	return m.phases
}

func (m mockRevisionResult) InTransistion() bool {
	return m.inTransition
}

func (m mockRevisionResult) IsComplete() bool {
	return m.isComplete
}

func (m mockRevisionResult) HasProgressed() bool {
	return m.hasProgressed
}

func (m mockRevisionResult) String() string {
	return "verbose full report..."
}

type mockPhaseResult struct {
	name            string
	validationError *validation.PhaseValidationError
	objects         []machinery.ObjectResult
	inTransition    bool
	isComplete      bool
	hasProgressed   bool
}

func (m mockPhaseResult) GetName() string {
	return m.name
}

func (m mockPhaseResult) GetValidationError() *validation.PhaseValidationError {
	return m.validationError
}

func (m mockPhaseResult) GetObjects() []machinery.ObjectResult {
	return m.objects
}

func (m mockPhaseResult) InTransistion() bool {
	return m.inTransition
}

func (m mockPhaseResult) IsComplete() bool {
	return m.isComplete
}

func (m mockPhaseResult) HasProgressed() bool {
	return m.hasProgressed
}

func (m mockPhaseResult) String() string {
	return "verbose phase report..."
}

type mockObjectResult struct {
	action  machinery.Action
	object  machinery.Object
	success bool
	probes  map[string]machinery.ObjectProbeResult
}

func (m mockObjectResult) Action() machinery.Action {
	return m.action
}

func (m mockObjectResult) Object() machinery.Object {
	return m.object
}

func (m mockObjectResult) Success() bool {
	return m.success
}

func (m mockObjectResult) Probes() map[string]machinery.ObjectProbeResult {
	return m.probes
}

func (m mockObjectResult) String() string {
	return "verbose object report..."
}

type mockRevisionTeardownResult struct {
	phases            []machinery.PhaseTeardownResult
	isComplete        bool
	waitingPhaseNames []string
}

func (m mockRevisionTeardownResult) GetPhases() []machinery.PhaseTeardownResult {
	return m.phases
}

func (m mockRevisionTeardownResult) IsComplete() bool {
	return m.isComplete
}

func (m mockRevisionTeardownResult) GetWaitingPhaseNames() []string {
	return m.waitingPhaseNames
}

func (m mockRevisionTeardownResult) GetActivePhaseName() (string, bool) {
	return "", false
}

func (m mockRevisionTeardownResult) GetGonePhaseNames() []string {
	return nil
}

func (m mockRevisionTeardownResult) String() string {
	return "verbose teardown report..."
}

type mockPhaseTeardownResult struct {
	name       string
	isComplete bool
}

func (m mockPhaseTeardownResult) GetName() string {
	return m.name
}

func (m mockPhaseTeardownResult) IsComplete() bool {
	return m.isComplete
}

func (m mockPhaseTeardownResult) Gone() []machinerytypes.ObjectRef {
	if m.isComplete {
		return []machinerytypes.ObjectRef{}
	}
	return nil
}

func (m mockPhaseTeardownResult) Waiting() []machinerytypes.ObjectRef {
	if !m.isComplete {
		return []machinerytypes.ObjectRef{}
	}
	return nil
}

func (m mockPhaseTeardownResult) String() string {
	return "verbose phase teardown report..."
}

func TestSummarizeRevisionResult_Nil(t *testing.T) {
	result := SummarizeRevisionResult(nil)
	assert.Empty(t, result)
}

func TestSummarizeRevisionResult_Success(t *testing.T) {
	result := SummarizeRevisionResult(mockRevisionResult{
		isComplete: true,
		phases: []machinery.PhaseResult{
			mockPhaseResult{
				name:       "deploy",
				isComplete: true,
			},
		},
	})
	assert.Equal(t, "reconcile completed successfully", result)
}

func TestSummarizeRevisionResult_ValidationError(t *testing.T) {
	verr := &validation.RevisionValidationError{
		RevisionName: "test",
	}
	result := SummarizeRevisionResult(mockRevisionResult{
		validationError: verr,
	})
	assert.Contains(t, result, "validation error")
}

func TestSummarizeRevisionResult_Collision(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "default",
		},
	}
	cm.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	result := SummarizeRevisionResult(mockRevisionResult{
		isComplete: false,
		phases: []machinery.PhaseResult{
			mockPhaseResult{
				name:       "deploy",
				isComplete: false,
				objects: []machinery.ObjectResult{
					mockObjectResult{
						action:  machinery.ActionCollision,
						object:  cm,
						success: false,
					},
				},
			},
		},
	})

	assert.Contains(t, result, "collision")
	assert.Contains(t, result, "ConfigMap")
	assert.Contains(t, result, "default/test-cm")
}

func TestSummarizeRevisionResult_ProbeFailure(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "default",
		},
	}
	cm.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	result := SummarizeRevisionResult(mockRevisionResult{
		isComplete: false,
		phases: []machinery.PhaseResult{
			mockPhaseResult{
				name:       "deploy",
				isComplete: false,
				objects: []machinery.ObjectResult{
					mockObjectResult{
						action:  "apply",
						object:  cm,
						success: true,
						probes: map[string]machinery.ObjectProbeResult{
							"progress": {
								Success:  false,
								Messages: []string{"not ready"},
							},
						},
					},
				},
			},
		},
	})

	assert.Contains(t, result, "probe failure")
	assert.Contains(t, result, "progress")
}

func TestSummarizeRevisionResult_MultipleCollisions(t *testing.T) {
	objects := []machinery.ObjectResult{}
	for i := 0; i < 5; i++ {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cm",
				Namespace: "default",
			},
		}
		cm.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "",
			Version: "v1",
			Kind:    "ConfigMap",
		})
		objects = append(objects, mockObjectResult{
			action:  machinery.ActionCollision,
			object:  cm,
			success: false,
		})
	}

	result := SummarizeRevisionResult(mockRevisionResult{
		isComplete: false,
		phases: []machinery.PhaseResult{
			mockPhaseResult{
				name:       "deploy",
				isComplete: false,
				objects:    objects,
			},
		},
	})

	// Should limit to first 3 and show total count
	assert.Contains(t, result, "5 collision(s)")
	assert.Contains(t, result, "showing first 3")
}

func TestSummarizeRevisionResult_InTransition(t *testing.T) {
	result := SummarizeRevisionResult(mockRevisionResult{
		inTransition: true,
		isComplete:   false,
	})
	assert.Contains(t, result, "in transition")
}

func TestSummarizeRevisionTeardownResult_Complete(t *testing.T) {
	result := SummarizeRevisionTeardownResult(mockRevisionTeardownResult{
		isComplete: true,
	})
	assert.Equal(t, "teardown completed successfully", result)
}

func TestSummarizeRevisionTeardownResult_WaitingPhases(t *testing.T) {
	result := SummarizeRevisionTeardownResult(mockRevisionTeardownResult{
		isComplete:        false,
		waitingPhaseNames: []string{"deploy", "configure"},
	})
	assert.Contains(t, result, "waiting on phases")
	assert.Contains(t, result, "deploy")
	assert.Contains(t, result, "configure")
}

func TestSummarizeRevisionTeardownResult_IncompletePhases(t *testing.T) {
	result := SummarizeRevisionTeardownResult(mockRevisionTeardownResult{
		isComplete: false,
		phases: []machinery.PhaseTeardownResult{
			mockPhaseTeardownResult{
				name:       "deploy",
				isComplete: false,
			},
			mockPhaseTeardownResult{
				name:       "configure",
				isComplete: true,
			},
		},
	})
	assert.Contains(t, result, "incomplete phases")
	assert.Contains(t, result, "deploy")
	assert.Contains(t, result, "1 phase(s) completed")
}

func TestGetObjectInfo_WithNamespace(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test-ns",
		},
	}
	cm.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	info := getObjectInfo(cm)
	assert.Equal(t, "ConfigMap test-ns/test-cm", info)
}

func TestGetObjectInfo_ClusterScoped(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cm",
		},
	}
	cm.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	info := getObjectInfo(cm)
	assert.Equal(t, "ConfigMap test-cm", info)
}

func TestGetObjectInfo_Nil(t *testing.T) {
	info := getObjectInfo(nil)
	assert.Equal(t, "unknown object", info)
}
