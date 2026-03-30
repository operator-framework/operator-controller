package applier

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
)

func TestExtractPhasesForPacking(t *testing.T) {
	obj := unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]interface{}{"name": "cm1"},
	}}

	cp := ocv1.CollisionProtectionPrevent
	acPhases := []ocv1ac.ClusterObjectSetPhaseApplyConfiguration{
		*ocv1ac.ClusterObjectSetPhase().WithName("deploy").WithObjects(
			ocv1ac.ClusterObjectSetObject().WithObject(obj).WithCollisionProtection(cp),
		),
	}

	result := extractPhasesForPacking(acPhases)

	require.Len(t, result, 1)
	assert.Equal(t, "deploy", result[0].Name)
	require.Len(t, result[0].Objects, 1)
	assert.Equal(t, "ConfigMap", result[0].Objects[0].Object.GetKind())
	assert.Equal(t, ocv1.CollisionProtectionPrevent, result[0].Objects[0].CollisionProtection)
}

func TestReplaceInlineWithRefs(t *testing.T) {
	obj1 := unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]interface{}{"name": "cm1"},
	}}
	obj2 := unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]interface{}{"name": "cm2"},
	}}

	rev := ocv1ac.ClusterObjectSet("test-rev").
		WithSpec(ocv1ac.ClusterObjectSetSpec().
			WithPhases(
				ocv1ac.ClusterObjectSetPhase().WithName("deploy").WithObjects(
					ocv1ac.ClusterObjectSetObject().WithObject(obj1),
					ocv1ac.ClusterObjectSetObject().WithObject(obj2),
				),
			),
		)

	pack := &PackResult{
		Refs: map[[2]int]ocv1.ObjectSourceRef{
			{0, 0}: {Name: "secret-1", Namespace: "olmv1-system", Key: "key-a"},
			{0, 1}: {Name: "secret-1", Namespace: "olmv1-system", Key: "key-b"},
		},
	}

	replaceInlineWithRefs(rev, pack)

	require.Len(t, rev.Spec.Phases, 1)
	require.Len(t, rev.Spec.Phases[0].Objects, 2)

	// Object should be nil, Ref should be set
	assert.Nil(t, rev.Spec.Phases[0].Objects[0].Object)
	require.NotNil(t, rev.Spec.Phases[0].Objects[0].Ref)
	assert.Equal(t, "secret-1", *rev.Spec.Phases[0].Objects[0].Ref.Name)
	assert.Equal(t, "olmv1-system", *rev.Spec.Phases[0].Objects[0].Ref.Namespace)
	assert.Equal(t, "key-a", *rev.Spec.Phases[0].Objects[0].Ref.Key)

	assert.Nil(t, rev.Spec.Phases[0].Objects[1].Object)
	require.NotNil(t, rev.Spec.Phases[0].Objects[1].Ref)
	assert.Equal(t, "secret-1", *rev.Spec.Phases[0].Objects[1].Ref.Name)
	assert.Equal(t, "key-b", *rev.Spec.Phases[0].Objects[1].Ref.Key)
}

func TestReplaceInlineWithRefs_NilSpec(t *testing.T) {
	rev := ocv1ac.ClusterObjectSet("test-rev")
	pack := &PackResult{
		Refs: map[[2]int]ocv1.ObjectSourceRef{},
	}
	// Should not panic
	replaceInlineWithRefs(rev, pack)
}
