package v1

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestClusterObjectSetImmutability(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	i := 0
	for name, tc := range map[string]struct {
		spec          ClusterObjectSetSpec
		updateFunc    func(*ClusterObjectSet)
		allowed       bool
		expectedError string
	}{
		"group is immutable": {
			spec: ClusterObjectSetSpec{
				Group:               "test-group",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.Group = "another-group"
			},
			expectedError: "group is immutable",
		},
		"group cannot be cleared": {
			spec: ClusterObjectSetSpec{
				Group:               "test-group",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.Group = ""
			},
			expectedError: "spec.group: Required value",
		},
		"unchanged group permits lifecycle update": {
			spec: ClusterObjectSetSpec{
				Group:               "test-group",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.Group = "test-group"
				cos.Spec.LifecycleState = ClusterObjectSetLifecycleStateArchived
			},
			allowed: true,
		},
		"revision is immutable": {
			spec: ClusterObjectSetSpec{
				Group:               "test-group",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.Revision = 2
			},
		},
		"phases may be initially empty": {
			spec: ClusterObjectSetSpec{
				Group:               "test-group",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
				Phases:              []ClusterObjectSetPhase{},
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.Phases = []ClusterObjectSetPhase{
					{
						Name:    "foo",
						Objects: []ClusterObjectSetObject{},
					},
				}
			},
			allowed: true,
		},
		"phases may be initially unset": {
			spec: ClusterObjectSetSpec{
				Group:               "test-group",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.Phases = []ClusterObjectSetPhase{
					{
						Name:    "foo",
						Objects: []ClusterObjectSetObject{},
					},
				}
			},
			allowed: true,
		},
		"phases are immutable if not empty": {
			spec: ClusterObjectSetSpec{
				Group:               "test-group",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
				Phases: []ClusterObjectSetPhase{
					{
						Name:    "foo",
						Objects: []ClusterObjectSetObject{},
					},
				},
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.Phases = []ClusterObjectSetPhase{
					{
						Name:    "foo2",
						Objects: []ClusterObjectSetObject{},
					},
				}
			},
		},
		"spec collisionProtection is immutable": {
			spec: ClusterObjectSetSpec{
				Group:               "test-group",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.CollisionProtection = CollisionProtectionNone
			},
		},
		"progressionProbes may be initially unset": {
			spec: ClusterObjectSetSpec{
				Group:               "test-group",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.ProgressionProbes = []ProgressionProbe{validProbe()}
			},
			allowed: true,
		},
		"progressionProbes are immutable once set": {
			spec: ClusterObjectSetSpec{
				Group:               "test-group",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
				ProgressionProbes:   []ProgressionProbe{validProbe()},
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.ProgressionProbes = []ProgressionProbe{validProbe(), validProbe()}
			},
			expectedError: "progressionProbes is immutable",
		},
		"lifecycleState cannot be un-archived": {
			spec: ClusterObjectSetSpec{
				Group:               "test-group",
				LifecycleState:      ClusterObjectSetLifecycleStateArchived,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.LifecycleState = ClusterObjectSetLifecycleStateActive
			},
		},
		"lifecycleState can transition from active to archived": {
			spec: ClusterObjectSetSpec{
				Group:               "test-group",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.LifecycleState = ClusterObjectSetLifecycleStateArchived
			},
			allowed: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			cos := &ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("foo%d", i),
				},
				Spec: tc.spec,
			}
			i = i + 1
			require.NoError(t, c.Create(ctx, cos))
			tc.updateFunc(cos)
			err := c.Update(ctx, cos)
			if tc.allowed && err != nil {
				t.Fatal("expected update to succeed, but got:", err)
			}
			if !tc.allowed && !errors.IsInvalid(err) {
				t.Fatal("expected update to fail due to invalid payload, but got:", err)
			}
			if tc.expectedError != "" {
				require.ErrorContains(t, err, tc.expectedError)
			}
		})
	}
}

func TestClusterObjectSetValidity(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	i := 0
	for name, tc := range map[string]struct {
		spec  ClusterObjectSetSpec
		valid bool
	}{
		"revision cannot be negative": {
			spec: ClusterObjectSetSpec{
				Group:          "validity-negative-revision",
				LifecycleState: ClusterObjectSetLifecycleStateActive,
				Revision:       -1,
			},
			valid: false,
		},
		"revision cannot be zero": {
			spec: ClusterObjectSetSpec{
				Group:          "validity-zero-revision",
				LifecycleState: ClusterObjectSetLifecycleStateActive,
			},
			valid: false,
		},
		"revision must be positive": {
			spec: ClusterObjectSetSpec{
				Group:               "validity-positive-revision",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			valid: true,
		},
		"lifecycleState must be set": {
			spec: ClusterObjectSetSpec{
				Group:    "validity-missing-lifecycle",
				Revision: 1,
			},
			valid: false,
		},
		"phases must have no more than 20 phases": {
			spec: ClusterObjectSetSpec{
				Group:          "validity-too-many-phases",
				LifecycleState: ClusterObjectSetLifecycleStateActive,
				Revision:       1,
				Phases:         make([]ClusterObjectSetPhase, 21),
			},
			valid: false,
		},
		"phases entries must have no more than 50 objects": {
			spec: ClusterObjectSetSpec{
				Group:          "validity-too-many-objects",
				LifecycleState: ClusterObjectSetLifecycleStateActive,
				Revision:       1,
				Phases: []ClusterObjectSetPhase{
					{
						Name:    "too-many-objects",
						Objects: make([]ClusterObjectSetObject, 51),
					},
				},
			},
			valid: false,
		},
		"phases entry names cannot be empty": {
			spec: ClusterObjectSetSpec{
				Group:          "validity-empty-phase-name",
				LifecycleState: ClusterObjectSetLifecycleStateActive,
				Revision:       1,
				Phases: []ClusterObjectSetPhase{
					{
						Name: "",
					},
				},
			},
			valid: false,
		},
		"phases entry names cannot start with symbols": {
			spec: ClusterObjectSetSpec{
				Group:          "validity-symbol-phase-name",
				LifecycleState: ClusterObjectSetLifecycleStateActive,
				Revision:       1,
				Phases: []ClusterObjectSetPhase{
					{
						Name: "-invalid",
					},
				},
			},
			valid: false,
		},
		"phases entry names cannot start with numeric characters": {
			spec: ClusterObjectSetSpec{
				Group:          "validity-numeric-phase-name",
				LifecycleState: ClusterObjectSetLifecycleStateActive,
				Revision:       1,
				Phases: []ClusterObjectSetPhase{
					{
						Name: "1-invalid",
					},
				},
			},
			valid: false,
		},
		"phases entry names cannot exceed 63 characters": {
			spec: ClusterObjectSetSpec{
				Group:               "test-group",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
				Phases: []ClusterObjectSetPhase{
					{
						Name:    strings.Repeat("a", 64),
						Objects: []ClusterObjectSetObject{},
					},
				},
			},
			valid: false,
		},
		"phases entry names must be unique": {
			spec: ClusterObjectSetSpec{
				Group:               "test-group",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
				Phases: []ClusterObjectSetPhase{
					{
						Name:    "deploy",
						Objects: []ClusterObjectSetObject{},
					},
					{
						Name:    "deploy",
						Objects: []ClusterObjectSetObject{},
					},
				},
			},
			valid: false,
		},
		"spec collisionProtection accepts Prevent": {
			spec: ClusterObjectSetSpec{
				Group:               "validity-prevent-collisions",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			valid: true,
		},
		"spec collisionProtection accepts IfNoController": {
			spec: ClusterObjectSetSpec{
				Group:               "validity-if-no-controller",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionIfNoController,
			},
			valid: true,
		},
		"spec collisionProtection accepts None": {
			spec: ClusterObjectSetSpec{
				Group:               "validity-allow-collisions",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionNone,
			},
			valid: true,
		},
		"spec collisionProtection is required": {
			spec: ClusterObjectSetSpec{
				Group:          "validity-required-collision-protection",
				LifecycleState: ClusterObjectSetLifecycleStateActive,
				Revision:       1,
			},
			valid: false,
		},
		"spec collisionProtection rejects invalid values": {
			spec: ClusterObjectSetSpec{
				Group:               "validity-invalid-collision-protection",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtection("Invalid"),
			},
			valid: false,
		},
		"spec collisionProtection must be set": {
			spec: ClusterObjectSetSpec{
				Group:          "validity-unset-collision-protection",
				LifecycleState: ClusterObjectSetLifecycleStateActive,
				Revision:       1,
			},
			valid: false,
		},
		"object collisionProtection is optional": {
			spec: ClusterObjectSetSpec{
				Group:               "validity-optional-object-collision-protection",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
				Phases: []ClusterObjectSetPhase{
					{
						Name: "deploy",
						Objects: []ClusterObjectSetObject{
							{
								Object: configMap(),
							},
						},
					},
				},
			},
			valid: true,
		},
		"object with inline object is valid": {
			spec: ClusterObjectSetSpec{
				Group:               "validity-inline-object",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
				Phases: []ClusterObjectSetPhase{
					{
						Name: "deploy",
						Objects: []ClusterObjectSetObject{
							{
								Object: configMap(),
							},
						},
					},
				},
			},
			valid: true,
		},
		"object with ref is valid": {
			spec: ClusterObjectSetSpec{
				Group:               "validity-ref-object",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
				Phases: []ClusterObjectSetPhase{
					{
						Name: "deploy",
						Objects: []ClusterObjectSetObject{
							{
								Ref: ObjectSourceRef{Name: "my-secret", Key: "my-key"},
							},
						},
					},
				},
			},
			valid: true,
		},
		"object ref requires a name": {
			spec:  refSpec(ObjectSourceRef{Key: "my-key"}),
			valid: false,
		},
		"object ref requires a key": {
			spec:  refSpec(ObjectSourceRef{Name: "my-secret"}),
			valid: false,
		},
		"object ref name cannot exceed 253 characters": {
			spec:  refSpec(ObjectSourceRef{Name: strings.Repeat("a", 254), Key: "my-key"}),
			valid: false,
		},
		"object ref namespace cannot exceed 63 characters": {
			spec:  refSpec(ObjectSourceRef{Name: "my-secret", Key: "my-key", Namespace: strings.Repeat("a", 64)}),
			valid: false,
		},
		"object with both object and ref is invalid": {
			spec: ClusterObjectSetSpec{
				Group:               "validity-object-and-ref",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
				Phases: []ClusterObjectSetPhase{
					{
						Name: "deploy",
						Objects: []ClusterObjectSetObject{
							{
								Object: configMap(),
								Ref:    ObjectSourceRef{Name: "my-secret", Key: "my-key"},
							},
						},
					},
				},
			},
			valid: false,
		},
		"object with neither object nor ref is invalid": {
			spec: ClusterObjectSetSpec{
				Group:               "validity-object-and-ref-missing",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
				Phases: []ClusterObjectSetPhase{
					{
						Name: "deploy",
						Objects: []ClusterObjectSetObject{
							{},
						},
					},
				},
			},
			valid: false,
		},
		"progressionProbe with GroupKind selector and ConditionEqual assertion is valid": {
			spec: probeSpec(ProgressionProbe{
				Selector:   validGroupKindSelector(),
				Assertions: []Assertion{validConditionEqualAssertion()},
			}),
			valid: true,
		},
		"progressionProbe with Label selector and FieldValue assertion is valid": {
			spec: probeSpec(ProgressionProbe{
				Selector: ObjectSelector{
					Type:  SelectorTypeLabel,
					Label: metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
				},
				Assertions: []Assertion{
					{
						Type:       ProbeTypeFieldValue,
						FieldValue: FieldValueProbe{FieldPath: "status.phase", Value: "Bound"},
					},
				},
			}),
			valid: true,
		},
		"progressionProbe rejects invalid selector type": {
			spec: probeSpec(ProgressionProbe{
				Selector:   ObjectSelector{Type: SelectorType("Invalid")},
				Assertions: []Assertion{validConditionEqualAssertion()},
			}),
			valid: false,
		},
		"progressionProbe GroupKind selector requires groupKind": {
			spec: probeSpec(ProgressionProbe{
				Selector:   ObjectSelector{Type: SelectorTypeGroupKind},
				Assertions: []Assertion{validConditionEqualAssertion()},
			}),
			valid: false,
		},
		"progressionProbe GroupKind selector forbids label": {
			spec: probeSpec(ProgressionProbe{
				Selector: ObjectSelector{
					Type:      SelectorTypeGroupKind,
					GroupKind: metav1.GroupKind{Kind: "ConfigMap"},
					Label:     metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
				},
				Assertions: []Assertion{validConditionEqualAssertion()},
			}),
			valid: false,
		},
		"progressionProbe Label selector requires label": {
			spec: probeSpec(ProgressionProbe{
				Selector:   ObjectSelector{Type: SelectorTypeLabel},
				Assertions: []Assertion{validConditionEqualAssertion()},
			}),
			valid: false,
		},
		"progressionProbe Label selector rejects both matchLabels and matchExpressions": {
			spec: probeSpec(ProgressionProbe{
				Selector: ObjectSelector{
					Type: SelectorTypeLabel,
					Label: metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{Key: "app", Operator: metav1.LabelSelectorOpExists},
						},
					},
				},
				Assertions: []Assertion{validConditionEqualAssertion()},
			}),
			valid: false,
		},
		"progressionProbe rejects invalid assertion type": {
			spec: probeSpec(ProgressionProbe{
				Selector:   validGroupKindSelector(),
				Assertions: []Assertion{{Type: ProbeType("Invalid")}},
			}),
			valid: false,
		},
		"progressionProbe ConditionEqual assertion requires conditionEqual": {
			spec: probeSpec(ProgressionProbe{
				Selector:   validGroupKindSelector(),
				Assertions: []Assertion{{Type: ProbeTypeConditionEqual}},
			}),
			valid: false,
		},
		"progressionProbe assertion forbids mismatched member": {
			spec: probeSpec(ProgressionProbe{
				Selector: validGroupKindSelector(),
				Assertions: []Assertion{
					{
						Type:           ProbeTypeFieldValue,
						FieldValue:     FieldValueProbe{FieldPath: "status.phase", Value: "Bound"},
						ConditionEqual: ConditionEqualProbe{Type: "Ready", Status: "True"},
					},
				},
			}),
			valid: false,
		},
		"progressionProbe ConditionEqual status must be True or False": {
			spec: probeSpec(ProgressionProbe{
				Selector: validGroupKindSelector(),
				Assertions: []Assertion{
					{
						Type:           ProbeTypeConditionEqual,
						ConditionEqual: ConditionEqualProbe{Type: "Ready", Status: "Unknown"},
					},
				},
			}),
			valid: false,
		},
		"progressionProbe FieldValue rejects invalid field path": {
			spec: probeSpec(ProgressionProbe{
				Selector: validGroupKindSelector(),
				Assertions: []Assertion{
					{
						Type:       ProbeTypeFieldValue,
						FieldValue: FieldValueProbe{FieldPath: "status..phase", Value: "Bound"},
					},
				},
			}),
			valid: false,
		},
		"progressionProbe FieldsEqual rejects invalid field path": {
			spec: probeSpec(ProgressionProbe{
				Selector: validGroupKindSelector(),
				Assertions: []Assertion{
					{
						Type:        ProbeTypeFieldsEqual,
						FieldsEqual: FieldsEqualProbe{FieldA: "spec..replicas", FieldB: "status.readyReplicas"},
					},
				},
			}),
			valid: false,
		},
		"progressionProbe requires at least one assertion": {
			spec: probeSpec(ProgressionProbe{
				Selector: validGroupKindSelector(),
			}),
			valid: false,
		},
		"progressionProbes must have no more than 20 probes": {
			spec: func() ClusterObjectSetSpec {
				probes := make([]ProgressionProbe, 21)
				for i := range probes {
					probes[i] = validProbe()
				}
				return probeSpec(probes...)
			}(),
			valid: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			cos := &ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("cos-validity-%d", i),
				},
				Spec: tc.spec,
			}
			i++
			err := c.Create(ctx, cos)
			if tc.valid && err != nil {
				t.Fatal("expected create to succeed, but got:", err)
			}
			if !tc.valid && !errors.IsInvalid(err) {
				t.Fatal("expected create to fail due to invalid payload, but got:", err)
			}
		})
	}
}

func TestClusterObjectSetSpecValidation(t *testing.T) {
	c := newClient(t)
	t.Run("missing spec", func(t *testing.T) {
		cos := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": GroupVersion.String(),
			"kind":       ClusterObjectSetKind,
			"metadata":   map[string]any{"name": "spec-validation-1"},
		}}
		err := c.Create(t.Context(), cos)
		require.True(t, errors.IsInvalid(err), "%v", err)
		require.ErrorContains(t, err, "spec: Required")
	})
}

func TestClusterObjectSetGroupValidation(t *testing.T) {
	c := newClient(t)
	for i, tc := range []struct {
		name  string
		group *string
		valid bool
	}{
		{name: "missing group", valid: false},
		{name: "empty group", group: ptr.To(""), valid: false},
		{name: "one character", group: ptr.To("a"), valid: true},
		{name: "lowercase hyphens and digits", group: ptr.To("my-group-1"), valid: true},
		{name: "maximum length", group: ptr.To(strings.Repeat("a", 52)), valid: true},
		{name: "over maximum length", group: ptr.To(strings.Repeat("a", 53)), valid: false},
		{name: "starts with digit", group: ptr.To("1group"), valid: false},
		{name: "starts with hyphen", group: ptr.To("-group"), valid: false},
		{name: "ends with hyphen", group: ptr.To("group-"), valid: false},
		{name: "uppercase", group: ptr.To("Group"), valid: false},
		{name: "underscore", group: ptr.To("my_group"), valid: false},
		{name: "dot", group: ptr.To("my.group"), valid: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objectName := fmt.Sprintf("group-validation-%d", i+1)
			if tc.valid {
				objectName = fmt.Sprintf("%s-1", *tc.group)
			}
			cos := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": GroupVersion.String(),
				"kind":       ClusterObjectSetKind,
				"metadata":   map[string]any{"name": objectName},
			}}
			spec := map[string]any{
				"revision": int64(1), "lifecycleState": string(ClusterObjectSetLifecycleStateActive),
				"collisionProtection": string(CollisionProtectionPrevent),
			}
			if tc.group != nil {
				spec["group"] = *tc.group
			}
			cos.Object["spec"] = spec
			err := c.Create(t.Context(), cos)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.True(t, errors.IsInvalid(err), "%v", err)
				require.ErrorContains(t, err, "spec.group")
			}
		})
	}
}

func TestClusterObjectSetGroupFieldSelector(t *testing.T) {
	// Use an uncached client so filtering is performed by the API server.
	c := newClient(t)
	for _, fixture := range []struct {
		name     string
		group    string
		revision int64
	}{
		{name: "field-selector-a-1", group: "field-selector-a", revision: 1},
		{name: "field-selector-a-2", group: "field-selector-a", revision: 2},
		{name: "field-selector-b-1", group: "field-selector-b", revision: 1},
	} {
		cos := &ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{Name: fixture.name},
			Spec: ClusterObjectSetSpec{
				Group:               fixture.group,
				Revision:            fixture.revision,
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				CollisionProtection: CollisionProtectionPrevent,
			},
		}
		require.NoError(t, c.Create(t.Context(), cos))
	}

	for _, tc := range []struct {
		group string
		want  []string
	}{
		{group: "field-selector-a", want: []string{"field-selector-a-1", "field-selector-a-2"}},
		{group: "field-selector-b", want: []string{"field-selector-b-1"}},
		{group: "field-selector-missing"},
	} {
		t.Run(tc.group, func(t *testing.T) {
			var list ClusterObjectSetList
			require.NoError(t, c.List(t.Context(), &list, client.MatchingFields{"spec.group": tc.group}))
			names := make([]string, 0, len(list.Items))
			for _, cos := range list.Items {
				names = append(names, cos.Name)
			}
			require.ElementsMatch(t, tc.want, names)
		})
	}
}

// probeSpec returns a minimal valid ClusterObjectSetSpec carrying the given progression probes,
// so that each case only varies the probe under test.
func probeSpec(probes ...ProgressionProbe) ClusterObjectSetSpec {
	return ClusterObjectSetSpec{
		Group:               "test-group",
		LifecycleState:      ClusterObjectSetLifecycleStateActive,
		Revision:            1,
		CollisionProtection: CollisionProtectionPrevent,
		ProgressionProbes:   probes,
	}
}

// validProbe returns a well-formed progression probe, used to exercise list-level
// constraints (e.g. MaxItems) without tripping any per-probe rule.
func validProbe() ProgressionProbe {
	return ProgressionProbe{
		Selector:   validGroupKindSelector(),
		Assertions: []Assertion{validConditionEqualAssertion()},
	}
}

// refSpec returns a minimal valid ClusterObjectSetSpec whose single phase object is sourced
// from the given ref, so that each ObjectSourceRef case only varies the ref under test.
func refSpec(ref ObjectSourceRef) ClusterObjectSetSpec {
	return ClusterObjectSetSpec{
		Group:               "test-group",
		LifecycleState:      ClusterObjectSetLifecycleStateActive,
		Revision:            1,
		CollisionProtection: CollisionProtectionPrevent,
		Phases: []ClusterObjectSetPhase{
			{
				Name:    "deploy",
				Objects: []ClusterObjectSetObject{{Ref: ref}},
			},
		},
	}
}

// validGroupKindSelector is a well-formed GroupKind selector, used when a case exercises the
// assertion union and needs the selector to be valid.
func validGroupKindSelector() ObjectSelector {
	return ObjectSelector{
		Type:      SelectorTypeGroupKind,
		GroupKind: metav1.GroupKind{Kind: "ConfigMap"},
	}
}

// validConditionEqualAssertion is a well-formed assertion, used when a case exercises the
// selector union and needs the assertion to be valid.
func validConditionEqualAssertion() Assertion {
	return Assertion{
		Type:           ProbeTypeConditionEqual,
		ConditionEqual: ConditionEqualProbe{Type: "Ready", Status: "True"}}
}

func configMap() unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name": "test-cm",
			},
		},
	}
}
