package v1

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestClusterObjectDeploymentValidity(t *testing.T) {
	c := newClient(t)
	for i, tc := range []struct {
		name   string
		mutate func(*ClusterObjectDeployment)
		valid  bool
	}{
		{name: "minimal template", valid: true},
		{name: "one character name", mutate: func(cod *ClusterObjectDeployment) { cod.Name = "a" }, valid: true},
		{name: "52 character name", mutate: func(cod *ClusterObjectDeployment) { cod.Name = strings.Repeat("a", 52) }, valid: true},
		{name: "53 character name", mutate: func(cod *ClusterObjectDeployment) { cod.Name = strings.Repeat("a", 53) }},
		{name: "name starts with a digit", mutate: func(cod *ClusterObjectDeployment) { cod.Name = "1-deployment" }},
		{name: "name contains a dot", mutate: func(cod *ClusterObjectDeployment) { cod.Name = "deployment.example" }},
		{name: "name contains uppercase", mutate: func(cod *ClusterObjectDeployment) { cod.Name = "Deployment" }},
		{name: "name ends with a hyphen", mutate: func(cod *ClusterObjectDeployment) { cod.Name = "deployment-" }},
		{name: "collision protection Prevent", mutate: codCollisionProtection(CollisionProtectionPrevent), valid: true},
		{name: "collision protection IfNoController", mutate: codCollisionProtection(CollisionProtectionIfNoController), valid: true},
		{name: "collision protection None", mutate: codCollisionProtection(CollisionProtectionNone), valid: true},
		{name: "collision protection invalid", mutate: codCollisionProtection("Invalid")},
		{name: "phases omitted", mutate: func(cod *ClusterObjectDeployment) { cod.Spec.Template.Spec.Phases = nil }},
		{name: "phases empty", mutate: func(cod *ClusterObjectDeployment) { cod.Spec.Template.Spec.Phases = []ClusterObjectSetPhase{} }},
		{name: "20 phases", mutate: codPhases(20), valid: true},
		{name: "21 phases", mutate: codPhases(21)},
		{name: "duplicate phase names", mutate: func(cod *ClusterObjectDeployment) {
			cod.Spec.Template.Spec.Phases = append(cod.Spec.Template.Spec.Phases, cod.Spec.Template.Spec.Phases[0])
		}},
		{name: "phase name empty", mutate: codPhaseName("")},
		{name: "phase name uppercase", mutate: codPhaseName("Deploy")},
		{name: "phase name contains dot", mutate: codPhaseName("deploy.example")},
		{name: "phase name ends with hyphen", mutate: codPhaseName("deploy-")},
		{name: "phase name 63 characters", mutate: codPhaseName(strings.Repeat("a", 63)), valid: true},
		{name: "phase name 64 characters", mutate: codPhaseName(strings.Repeat("a", 64))},
		{name: "phase with 50 objects", mutate: codPhaseObjects(50), valid: true},
		{name: "phase with 51 objects", mutate: codPhaseObjects(51)},
		{name: "phase collision protection invalid", mutate: func(cod *ClusterObjectDeployment) {
			cod.Spec.Template.Spec.Phases[0].CollisionProtection = "Invalid"
		}},
		{name: "object collision protection invalid", mutate: func(cod *ClusterObjectDeployment) {
			cod.Spec.Template.Spec.Phases[0].Objects[0].CollisionProtection = "Invalid"
		}},
		{name: "object reference", mutate: func(cod *ClusterObjectDeployment) {
			cod.Spec.Template.Spec.Phases[0].Objects[0] = ClusterObjectSetObject{Ref: ObjectSourceRef{Name: "objects", Key: "configmap"}}
		}, valid: true},
		{name: "both inline object and reference", mutate: func(cod *ClusterObjectDeployment) {
			cod.Spec.Template.Spec.Phases[0].Objects[0].Ref = ObjectSourceRef{Name: "objects", Key: "configmap"}
		}},
		{name: "neither inline object nor reference", mutate: func(cod *ClusterObjectDeployment) {
			cod.Spec.Template.Spec.Phases[0].Objects[0] = ClusterObjectSetObject{}
		}},
		{name: "reference key missing", mutate: func(cod *ClusterObjectDeployment) {
			cod.Spec.Template.Spec.Phases[0].Objects[0] = ClusterObjectSetObject{Ref: ObjectSourceRef{Name: "objects"}}
		}},
		{name: "inline object kind missing", mutate: func(cod *ClusterObjectDeployment) {
			cod.Spec.Template.Spec.Phases[0].Objects[0].Object.SetKind("")
		}},
		{name: "progression probe", mutate: func(cod *ClusterObjectDeployment) {
			cod.Spec.Template.Spec.ProgressionProbes = []ProgressionProbe{codProbe("ready")}
		}, valid: true},
		{name: "progression probe selector union invalid", mutate: func(cod *ClusterObjectDeployment) {
			probe := codProbe("ready")
			probe.Selector.Label = metav1.LabelSelector{MatchLabels: map[string]string{"app": "example"}}
			cod.Spec.Template.Spec.ProgressionProbes = []ProgressionProbe{probe}
		}},
		{name: "progression probe assertion union invalid", mutate: func(cod *ClusterObjectDeployment) {
			probe := codProbe("ready")
			probe.Assertions[0].ConditionEqual = ConditionEqualProbe{Type: "Ready", Status: "True"}
			cod.Spec.Template.Spec.ProgressionProbes = []ProgressionProbe{probe}
		}},
		{name: "progression probe assertions missing", mutate: func(cod *ClusterObjectDeployment) {
			probe := codProbe("ready")
			probe.Assertions = nil
			cod.Spec.Template.Spec.ProgressionProbes = []ProgressionProbe{probe}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cod := newTestClusterObjectDeployment(fmt.Sprintf("cod-validity-%d", i))
			if tc.mutate != nil {
				tc.mutate(cod)
			}
			err := c.Create(t.Context(), cod)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.True(t, apierrors.IsInvalid(err), "expected invalid deployment, got: %v", err)
			}
		})
	}
}

func TestClusterObjectDeploymentDoesNotDefaultTemplate(t *testing.T) {
	c := newClient(t)
	cod := newTestClusterObjectDeployment("cod-no-defaults")
	require.NoError(t, c.Create(t.Context(), cod))
	cod = getTestClusterObjectDeployment(t, c, cod.Name)
	require.Empty(t, cod.Spec.Template.Spec.CollisionProtection)
	require.Empty(t, cod.Spec.Template.Spec.Phases[0].CollisionProtection)
	require.Empty(t, cod.Spec.Template.Spec.Phases[0].Objects[0].CollisionProtection)

	// Omitted and explicitly supplied Prevent remain distinguishable templates.
	cod.Spec.Template.Spec.CollisionProtection = CollisionProtectionPrevent
	require.NoError(t, c.Update(t.Context(), cod))
	cod = getTestClusterObjectDeployment(t, c, cod.Name)
	require.Equal(t, CollisionProtectionPrevent, cod.Spec.Template.Spec.CollisionProtection)
	cod.Spec.Template.Spec.CollisionProtection = ""
	require.NoError(t, c.Update(t.Context(), cod))
	cod = getTestClusterObjectDeployment(t, c, cod.Name)
	require.Empty(t, cod.Spec.Template.Spec.CollisionProtection)
}

func TestClusterObjectDeploymentTemplateIsMutable(t *testing.T) {
	c := newClient(t)
	cod := newTestClusterObjectDeployment("cod-mutable-template")
	require.NoError(t, c.Create(t.Context(), cod))
	initialGeneration := cod.Generation

	secondObject := configMap()
	secondObject.SetName("second-cm")
	cod.Spec.Template.Spec.Phases = append(cod.Spec.Template.Spec.Phases, ClusterObjectSetPhase{
		Name: "second", Objects: []ClusterObjectSetObject{{Object: secondObject}},
	})
	cod.Spec.Template.Spec.CollisionProtection = CollisionProtectionIfNoController
	cod.Spec.Template.Spec.ProgressionProbes = []ProgressionProbe{codProbe("first"), codProbe("second")}
	require.NoError(t, c.Update(t.Context(), cod))
	require.Greater(t, cod.Generation, initialGeneration)

	// COS immutability rules must not apply to the mutable deployment template.
	cod.Spec.Template.Spec.Phases[0], cod.Spec.Template.Spec.Phases[1] = cod.Spec.Template.Spec.Phases[1], cod.Spec.Template.Spec.Phases[0]
	cod.Spec.Template.Spec.Phases[0].Name = "renamed"
	cod.Spec.Template.Spec.Phases[0].Objects[0].Object.Object["data"] = map[string]interface{}{"mode": "updated"}
	cod.Spec.Template.Spec.CollisionProtection = CollisionProtectionNone
	cod.Spec.Template.Spec.ProgressionProbes[0], cod.Spec.Template.Spec.ProgressionProbes[1] = cod.Spec.Template.Spec.ProgressionProbes[1], cod.Spec.Template.Spec.ProgressionProbes[0]
	cod.Spec.Template.Spec.ProgressionProbes[0].Assertions[0].FieldValue.Value = "updated"
	expected := cod.Spec.DeepCopy()
	require.NoError(t, c.Update(t.Context(), cod))
	cod = getTestClusterObjectDeployment(t, c, cod.Name)
	require.Equal(t, *expected, cod.Spec)

	// Optional fields can also be removed after being set.
	cod.Spec.Template.Spec.ProgressionProbes = nil
	cod.Spec.Template.Spec.CollisionProtection = ""
	require.NoError(t, c.Update(t.Context(), cod))
}

func TestClusterObjectDeploymentUpdateValidation(t *testing.T) {
	c := newClient(t)
	for i, tc := range []struct {
		name   string
		mutate func(*ClusterObjectDeployment)
	}{
		{name: "no phases", mutate: func(cod *ClusterObjectDeployment) { cod.Spec.Template.Spec.Phases = nil }},
		{name: "invalid collision protection", mutate: codCollisionProtection("Invalid")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cod := newTestClusterObjectDeployment(fmt.Sprintf("cod-update-validity-%d", i))
			require.NoError(t, c.Create(t.Context(), cod))
			expected := cod.Spec.DeepCopy()
			tc.mutate(cod)
			err := c.Update(t.Context(), cod)
			require.True(t, apierrors.IsInvalid(err), "expected invalid deployment update, got: %v", err)
			cod = getTestClusterObjectDeployment(t, c, cod.Name)
			require.Equal(t, *expected, cod.Spec)
		})
	}
}

func TestClusterObjectDeploymentStatusSubresource(t *testing.T) {
	c := newClient(t)
	cod := newTestClusterObjectDeployment("cod-status-subresource")
	cod.Status = codTestStatus()
	require.NoError(t, c.Create(t.Context(), cod))
	cod = getTestClusterObjectDeployment(t, c, cod.Name)
	require.Equal(t, ClusterObjectDeploymentStatus{}, cod.Status, "create must discard status")

	cod.Status = codTestStatus()
	require.NoError(t, c.Update(t.Context(), cod))
	cod = getTestClusterObjectDeployment(t, c, cod.Name)
	require.Equal(t, ClusterObjectDeploymentStatus{}, cod.Status, "ordinary update must preserve existing status")

	expectedSpec := cod.Spec.DeepCopy()
	cod.Spec.Template.Spec.CollisionProtection = CollisionProtectionNone
	cod.Status = codTestStatus()
	expectedStatus := cod.Status.DeepCopy()
	require.NoError(t, c.Status().Update(t.Context(), cod))
	cod = getTestClusterObjectDeployment(t, c, cod.Name)
	require.Equal(t, *expectedSpec, cod.Spec, "status update must preserve spec")
	require.True(t, apiequality.Semantic.DeepEqual(*expectedStatus, cod.Status), "status must survive storage")

	cod.Status = ClusterObjectDeploymentStatus{}
	cod.Spec.Template.Spec.CollisionProtection = CollisionProtectionPrevent
	require.NoError(t, c.Update(t.Context(), cod))
	cod = getTestClusterObjectDeployment(t, c, cod.Name)
	require.True(t, apiequality.Semantic.DeepEqual(*expectedStatus, cod.Status), "ordinary update cannot clear status")
	require.Equal(t, CollisionProtectionPrevent, cod.Spec.Template.Spec.CollisionProtection)
}

func TestClusterObjectDeploymentTemplateSchema(t *testing.T) {
	c := newClient(t)
	for i, path := range [][]string{
		{"spec"},
		{"spec", "template"},
		{"spec", "template", "spec"},
		{"spec", "template", "spec", "phases"},
	} {
		t.Run("required "+strings.Join(path, "."), func(t *testing.T) {
			cod := newTestClusterObjectDeployment(fmt.Sprintf("cod-required-%d", i))
			raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cod)
			require.NoError(t, err)
			unstructured.RemoveNestedField(raw, path...)
			err = c.Create(t.Context(), &unstructured.Unstructured{Object: raw})
			require.True(t, apierrors.IsInvalid(err), "expected missing required field to be invalid, got: %v", err)
		})
	}

	t.Run("controller assigned fields are absent", func(t *testing.T) {
		cod := newTestClusterObjectDeployment("cod-template-pruning")
		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cod)
		require.NoError(t, err)
		for field, value := range map[string]interface{}{
			"group": "example", "revision": int64(42), "lifecycleState": "Active", "progressDeadlineMinutes": int64(10),
		} {
			require.NoError(t, unstructured.SetNestedField(raw, value, "spec", "template", "spec", field))
		}
		obj := &unstructured.Unstructured{Object: raw}
		require.NoError(t, c.Create(t.Context(), obj))
		require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
		for _, field := range []string{"group", "revision", "lifecycleState", "progressDeadlineMinutes"} {
			_, found, err := unstructured.NestedFieldNoCopy(obj.Object, "spec", "template", "spec", field)
			require.NoError(t, err)
			require.False(t, found, "template must not expose %s", field)
		}
	})
}

func TestClusterObjectDeploymentStatusValidation(t *testing.T) {
	c := newClient(t)
	cod := newTestClusterObjectDeployment("cod-status-validity")
	require.NoError(t, c.Create(t.Context(), cod))
	cod.Status = codTestStatus()
	cod.Status.Conditions = append(cod.Status.Conditions, cod.Status.Conditions[0])
	err := c.Status().Update(t.Context(), cod)
	require.True(t, apierrors.IsInvalid(err), "expected duplicate deployment conditions to be invalid, got: %v", err)
}

func TestClusterObjectDeploymentDeferredFieldsArePruned(t *testing.T) {
	c := newClient(t)
	cod := newTestClusterObjectDeployment("cod-deferred-fields")
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cod)
	require.NoError(t, err)
	require.NoError(t, unstructured.SetNestedField(raw, int64(10), "spec", "progressDeadlineMinutes"))
	require.NoError(t, unstructured.SetNestedField(raw, map[string]interface{}{
		"labels":      map[string]interface{}{"app": "example"},
		"annotations": map[string]interface{}{"note": "deferred"},
	}, "spec", "template", "metadata"))
	obj := &unstructured.Unstructured{Object: raw}
	require.NoError(t, c.Create(t.Context(), obj))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
	for _, path := range [][]string{{"spec", "progressDeadlineMinutes"}, {"spec", "template", "metadata"}} {
		_, found, err := unstructured.NestedFieldNoCopy(obj.Object, path...)
		require.NoError(t, err)
		require.False(t, found, "deployment must not expose %s", strings.Join(path, "."))
	}

	status := codTestStatus()
	rawStatus, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&status)
	require.NoError(t, err)
	rawStatus["activeRevisions"] = []interface{}{map[string]interface{}{"name": "cod-deferred-fields-1"}}
	rawStatus["objectCounts"] = map[string]interface{}{"total": int64(0)}
	obj.Object["status"] = rawStatus
	require.NoError(t, c.Status().Update(t.Context(), obj))
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(obj), obj))
	for _, field := range []string{"activeRevisions", "objectCounts"} {
		_, found, err := unstructured.NestedFieldNoCopy(obj.Object, "status", field)
		require.NoError(t, err)
		require.False(t, found, "deployment status must not expose %s", field)
	}
}

func getTestClusterObjectDeployment(t *testing.T, c client.Client, name string) *ClusterObjectDeployment {
	t.Helper()
	cod := &ClusterObjectDeployment{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: name}, cod))
	return cod
}

func newTestClusterObjectDeployment(name string) *ClusterObjectDeployment {
	return &ClusterObjectDeployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: "ClusterObjectDeployment"},
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: ClusterObjectDeploymentSpec{
			Template: ClusterObjectDeploymentTemplate{
				Spec: ClusterObjectDeploymentTemplateSpec{
					Phases: []ClusterObjectSetPhase{{Name: "deploy", Objects: []ClusterObjectSetObject{{Object: configMap()}}}},
				},
			},
		},
	}
}

func codCollisionProtection(value CollisionProtection) func(*ClusterObjectDeployment) {
	return func(cod *ClusterObjectDeployment) { cod.Spec.Template.Spec.CollisionProtection = value }
}

func codPhaseName(name string) func(*ClusterObjectDeployment) {
	return func(cod *ClusterObjectDeployment) { cod.Spec.Template.Spec.Phases[0].Name = name }
}

func codPhases(count int) func(*ClusterObjectDeployment) {
	return func(cod *ClusterObjectDeployment) {
		cod.Spec.Template.Spec.Phases = make([]ClusterObjectSetPhase, count)
		for i := range cod.Spec.Template.Spec.Phases {
			cod.Spec.Template.Spec.Phases[i] = ClusterObjectSetPhase{
				Name: fmt.Sprintf("phase-%d", i), Objects: []ClusterObjectSetObject{{Object: configMap()}},
			}
		}
	}
}

func codPhaseObjects(count int) func(*ClusterObjectDeployment) {
	return func(cod *ClusterObjectDeployment) {
		objects := make([]ClusterObjectSetObject, count)
		for i := range objects {
			obj := configMap()
			obj.SetName(fmt.Sprintf("configmap-%d", i))
			objects[i] = ClusterObjectSetObject{Object: obj}
		}
		cod.Spec.Template.Spec.Phases[0].Objects = objects
	}
}

func codProbe(value string) ProgressionProbe {
	return ProgressionProbe{
		Selector: ObjectSelector{Type: SelectorTypeGroupKind, GroupKind: metav1.GroupKind{Kind: "ConfigMap"}},
		Assertions: []Assertion{{
			Type:       ProbeTypeFieldValue,
			FieldValue: FieldValueProbe{FieldPath: "data.mode", Value: value},
		}},
	}
}

func codTestStatus() ClusterObjectDeploymentStatus {
	condition := metav1.Condition{
		Type: ClusterObjectDeploymentTypeProgressing, Status: metav1.ConditionTrue,
		Reason: ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, Message: "revision is progressing",
		ObservedGeneration: 1, LastTransitionTime: metav1.NewTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
	}
	return ClusterObjectDeploymentStatus{
		Conditions: []metav1.Condition{condition},
	}
}
