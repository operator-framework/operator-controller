package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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
	for name, tc := range map[string]struct {
		spec          ClusterObjectSetSpec
		updateFunc    func(*ClusterObjectSet)
		allowed       bool
		expectedError string
	}{
		"group is immutable": {
			spec: ClusterObjectSetSpec{
				Group:               "immutability-group-change",
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
				Group:               "immutability-group-cleared",
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
				Group:               "immutability-lifecycle-update",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.LifecycleState = ClusterObjectSetLifecycleStateArchived
			},
			allowed: true,
		},
		"revision is immutable": {
			spec: ClusterObjectSetSpec{
				Group:               "immutability-revision-change",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.Revision = 2
			},
			expectedError: "revision is immutable",
		},
		"phases may be initially empty": {
			spec: ClusterObjectSetSpec{
				Group:               "immutability-empty-phases",
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
				Group:               "immutability-unset-phases",
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
				Group:               "immutability-populated-phases",
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
			expectedError: "phases is immutable",
		},
		"spec collisionProtection is immutable": {
			spec: ClusterObjectSetSpec{
				Group:               "immutability-collision-protection",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.CollisionProtection = CollisionProtectionNone
			},
			expectedError: "collisionProtection is immutable",
		},
		"progressionProbes may be initially unset": {
			spec: ClusterObjectSetSpec{
				Group:               "immutability-unset-probes",
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
				Group:               "immutability-populated-probes",
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
				Group:               "immutability-unarchive",
				LifecycleState:      ClusterObjectSetLifecycleStateArchived,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.LifecycleState = ClusterObjectSetLifecycleStateActive
			},
			expectedError: "cannot un-archive",
		},
		"lifecycleState can transition from active to archived": {
			spec: ClusterObjectSetSpec{
				Group:               "immutability-archive",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.LifecycleState = ClusterObjectSetLifecycleStateArchived
			},
			allowed: true,
		},
		"metadata can be updated": {
			spec: ClusterObjectSetSpec{
				Group:               "immutability-metadata",
				Revision:            1,
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Labels = map[string]string{"test": "updated"}
				cos.Annotations = map[string]string{"test": "updated"}
			},
			allowed: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			cos := &ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%d", tc.spec.Group, tc.spec.Revision),
				},
				Spec: tc.spec,
			}
			require.NoError(t, c.Create(ctx, cos))
			tc.updateFunc(cos)
			err := c.Update(ctx, cos)
			if tc.allowed {
				require.NoError(t, err)
				return
			}
			require.True(t, errors.IsInvalid(err), "%v", err)
			require.NotEmpty(t, tc.expectedError)
			require.ErrorContains(t, err, tc.expectedError)
		})
	}
}

func TestClusterObjectSetValidity(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	for name, tc := range map[string]struct {
		spec          ClusterObjectSetSpec
		valid         bool
		expectedError string
	}{
		"lifecycleState must be set": {
			spec: ClusterObjectSetSpec{
				CollisionProtection: CollisionProtectionPrevent,
				Group:               "validity-missing-lifecycle",
				Revision:            1,
			},
			valid:         false,
			expectedError: "spec.lifecycleState: Required value",
		},
		"phases must have no more than 20 phases": {
			spec: ClusterObjectSetSpec{
				CollisionProtection: CollisionProtectionPrevent,
				Group:               "validity-too-many-phases",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				Phases: func() []ClusterObjectSetPhase {
					phases := make([]ClusterObjectSetPhase, 21)
					for i := range phases {
						phases[i] = ClusterObjectSetPhase{Name: fmt.Sprintf("phase-%d", i), Objects: []ClusterObjectSetObject{}}
					}
					return phases
				}(),
			},
			valid:         false,
			expectedError: "spec.phases: Too many",
		},
		"phases entries must have no more than 50 objects": {
			spec: ClusterObjectSetSpec{
				CollisionProtection: CollisionProtectionPrevent,
				Group:               "validity-too-many-objects",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				Phases: []ClusterObjectSetPhase{
					{
						Name: "too-many-objects",
						Objects: func() []ClusterObjectSetObject {
							objects := make([]ClusterObjectSetObject, 51)
							for i := range objects {
								objects[i] = ClusterObjectSetObject{Object: configMap()}
							}
							return objects
						}(),
					},
				},
			},
			valid:         false,
			expectedError: "spec.phases[0].objects: Too many",
		},
		"phases entry names cannot be empty": {
			spec: ClusterObjectSetSpec{
				CollisionProtection: CollisionProtectionPrevent,
				Group:               "validity-empty-phase-name",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				Phases: []ClusterObjectSetPhase{
					{
						Name:    "",
						Objects: []ClusterObjectSetObject{},
					},
				},
			},
			valid:         false,
			expectedError: "spec.phases[0].name: Invalid value",
		},
		"phases entry names cannot start with symbols": {
			spec: ClusterObjectSetSpec{
				CollisionProtection: CollisionProtectionPrevent,
				Group:               "validity-symbol-phase-name",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				Phases: []ClusterObjectSetPhase{
					{
						Name:    "-invalid",
						Objects: []ClusterObjectSetObject{},
					},
				},
			},
			valid:         false,
			expectedError: "the value must consist of only lowercase alphanumeric",
		},
		"phases entry names can start with numeric characters": {
			spec: ClusterObjectSetSpec{
				CollisionProtection: CollisionProtectionPrevent,
				Group:               "validity-numeric-phase-name",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				Phases: []ClusterObjectSetPhase{
					{
						Name:    "1-valid",
						Objects: []ClusterObjectSetObject{},
					},
				},
			},
			valid: true,
		},
		"phases entry names cannot exceed 63 characters": {
			spec: ClusterObjectSetSpec{
				Group:               "validity-case-10",
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
			valid:         false,
			expectedError: "spec.phases[0].name: Too long",
		},
		"phases entry names must be unique": {
			spec: ClusterObjectSetSpec{
				Group:               "validity-case-11",
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
			valid:         false,
			expectedError: "spec.phases[1]: Duplicate value",
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
		"spec collisionProtection rejects empty values": {
			spec: ClusterObjectSetSpec{
				Group:          "validity-empty-collision-protection",
				LifecycleState: ClusterObjectSetLifecycleStateActive,
				Revision:       1,
			},
			valid:         false,
			expectedError: "spec.collisionProtection: Unsupported value",
		},
		"spec collisionProtection rejects invalid values": {
			spec: ClusterObjectSetSpec{
				Group:               "validity-invalid-collision-protection",
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtection("Invalid"),
			},
			valid:         false,
			expectedError: "spec.collisionProtection: Unsupported value",
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
			spec:          refSpec("validity-case-21", ObjectSourceRef{Key: "my-key"}),
			valid:         false,
			expectedError: "ref.name: Invalid value",
		},
		"object ref requires a key": {
			spec:          refSpec("validity-case-22", ObjectSourceRef{Name: "my-secret"}),
			valid:         false,
			expectedError: "ref.key: Invalid value",
		},
		"object ref name cannot exceed 253 characters": {
			spec:          refSpec("validity-case-23", ObjectSourceRef{Name: strings.Repeat("a", 254), Key: "my-key"}),
			valid:         false,
			expectedError: "ref.name: Too long",
		},
		"object ref namespace cannot exceed 63 characters": {
			spec:          refSpec("validity-case-24", ObjectSourceRef{Name: "my-secret", Key: "my-key", Namespace: strings.Repeat("a", 64)}),
			valid:         false,
			expectedError: "ref.namespace: Too long",
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
			valid:         false,
			expectedError: "exactly one of the fields in [object ref] must be set",
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
			valid:         false,
			expectedError: "exactly one of the fields in [object ref] must be set",
		},
		"progressionProbe with GroupKind selector and ConditionEqual assertion is valid": {
			spec: probeSpec("validity-case-27", ProgressionProbe{
				Selector:   validGroupKindSelector(),
				Assertions: []Assertion{validConditionEqualAssertion()},
			}),
			valid: true,
		},
		"progressionProbe with Label selector and FieldValue assertion is valid": {
			spec: probeSpec("validity-case-28", ProgressionProbe{
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
			spec: probeSpec("validity-case-29", ProgressionProbe{
				Selector:   ObjectSelector{Type: SelectorType("Invalid")},
				Assertions: []Assertion{validConditionEqualAssertion()},
			}),
			valid:         false,
			expectedError: "selector.type: Unsupported value",
		},
		"progressionProbe GroupKind selector requires groupKind": {
			spec: probeSpec("validity-case-30", ProgressionProbe{
				Selector:   ObjectSelector{Type: SelectorTypeGroupKind},
				Assertions: []Assertion{validConditionEqualAssertion()},
			}),
			valid:         false,
			expectedError: "groupKind is required when type is GroupKind",
		},
		"progressionProbe GroupKind selector forbids label": {
			spec: probeSpec("validity-case-31", ProgressionProbe{
				Selector: ObjectSelector{
					Type:      SelectorTypeGroupKind,
					GroupKind: metav1.GroupKind{Kind: "ConfigMap"},
					Label:     metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
				},
				Assertions: []Assertion{validConditionEqualAssertion()},
			}),
			valid:         false,
			expectedError: "label is required when type is Label, and forbidden otherwise",
		},
		"progressionProbe Label selector requires label": {
			spec: probeSpec("validity-case-32", ProgressionProbe{
				Selector:   ObjectSelector{Type: SelectorTypeLabel},
				Assertions: []Assertion{validConditionEqualAssertion()},
			}),
			valid:         false,
			expectedError: "label is required when type is Label",
		},
		"progressionProbe Label selector rejects both matchLabels and matchExpressions": {
			spec: probeSpec("validity-case-33", ProgressionProbe{
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
			valid:         false,
			expectedError: "exactly one of matchLabels or matchExpressions must be set",
		},
		"progressionProbe rejects invalid assertion type": {
			spec: probeSpec("validity-case-34", ProgressionProbe{
				Selector:   validGroupKindSelector(),
				Assertions: []Assertion{{Type: ProbeType("Invalid")}},
			}),
			valid:         false,
			expectedError: "assertions[0].type: Unsupported value",
		},
		"progressionProbe ConditionEqual assertion requires conditionEqual": {
			spec: probeSpec("validity-case-35", ProgressionProbe{
				Selector:   validGroupKindSelector(),
				Assertions: []Assertion{{Type: ProbeTypeConditionEqual}},
			}),
			valid:         false,
			expectedError: "conditionEqual is required when type is ConditionEqual",
		},
		"progressionProbe assertion forbids mismatched member": {
			spec: probeSpec("validity-case-36", ProgressionProbe{
				Selector: validGroupKindSelector(),
				Assertions: []Assertion{
					{
						Type:           ProbeTypeFieldValue,
						FieldValue:     FieldValueProbe{FieldPath: "status.phase", Value: "Bound"},
						ConditionEqual: ConditionEqualProbe{Type: "Ready", Status: "True"},
					},
				},
			}),
			valid:         false,
			expectedError: "conditionEqual is required when type is ConditionEqual, and forbidden otherwise",
		},
		"progressionProbe ConditionEqual status must be True or False": {
			spec: probeSpec("validity-case-37", ProgressionProbe{
				Selector: validGroupKindSelector(),
				Assertions: []Assertion{
					{
						Type:           ProbeTypeConditionEqual,
						ConditionEqual: ConditionEqualProbe{Type: "Ready", Status: "Unknown"},
					},
				},
			}),
			valid:         false,
			expectedError: "conditionEqual.status: Unsupported value",
		},
		"progressionProbe FieldValue rejects invalid field path": {
			spec: probeSpec("validity-case-38", ProgressionProbe{
				Selector: validGroupKindSelector(),
				Assertions: []Assertion{
					{
						Type:       ProbeTypeFieldValue,
						FieldValue: FieldValueProbe{FieldPath: "status..phase", Value: "Bound"},
					},
				},
			}),
			valid:         false,
			expectedError: "must contain a valid field path",
		},
		"progressionProbe FieldsEqual rejects invalid field path": {
			spec: probeSpec("validity-case-39", ProgressionProbe{
				Selector: validGroupKindSelector(),
				Assertions: []Assertion{
					{
						Type:        ProbeTypeFieldsEqual,
						FieldsEqual: FieldsEqualProbe{FieldA: "spec..replicas", FieldB: "status.readyReplicas"},
					},
				},
			}),
			valid:         false,
			expectedError: "must contain a valid field path",
		},
		"progressionProbe requires at least one assertion": {
			spec: probeSpec("validity-case-40", ProgressionProbe{
				Selector: validGroupKindSelector(),
			}),
			valid:         false,
			expectedError: "assertions: Required value",
		},
		"progressionProbes must have no more than 20 probes": {
			spec: func() ClusterObjectSetSpec {
				probes := make([]ProgressionProbe, 21)
				for i := range probes {
					probes[i] = validProbe()
				}
				return probeSpec("validity-case-41", probes...)
			}(),
			valid:         false,
			expectedError: "spec.progressionProbes: Too many",
		},
	} {
		t.Run(name, func(t *testing.T) {
			cos := &ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%d", tc.spec.Group, tc.spec.Revision),
				},
				Spec: tc.spec,
			}
			err := c.Create(ctx, cos)
			if tc.valid {
				require.NoError(t, err)
				return
			}
			require.True(t, errors.IsInvalid(err), "%v", err)
			require.NotEmpty(t, tc.expectedError)
			require.ErrorContains(t, err, tc.expectedError)
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

func TestClusterObjectSetRevisionBounds(t *testing.T) {
	c := newClient(t)
	for _, tc := range []struct {
		testName      string
		revision      *int64
		valid         bool
		expectedError string
	}{
		{testName: "missing", expectedError: "spec.revision: Required value"},
		{testName: "negative", revision: ptr.To(int64(-1)), expectedError: "spec.revision: Invalid value"},
		{testName: "zero", revision: ptr.To(int64(0)), expectedError: "spec.revision: Invalid value"},
		{testName: "minimum", revision: ptr.To(int64(1)), valid: true},
		{testName: "maximum", revision: ptr.To(ClusterObjectSetMaxRevision), valid: true},
		{testName: "above maximum", revision: ptr.To(ClusterObjectSetMaxRevision + 1), expectedError: "spec.revision: Invalid value"},
		{testName: "maximum int64", revision: ptr.To(int64(math.MaxInt64)), expectedError: "spec.revision: Invalid value"},
	} {
		t.Run(tc.testName, func(t *testing.T) {
			// Both accepted boundaries must fit with a maximum-length group.
			group := "bounds-" + strings.Repeat("a", 45)
			spec := map[string]any{
				"group":               group,
				"lifecycleState":      string(ClusterObjectSetLifecycleStateActive),
				"collisionProtection": string(CollisionProtectionPrevent),
			}
			name := group + "-1"
			if tc.revision != nil {
				spec["revision"] = *tc.revision
				name = fmt.Sprintf("%s-%d", group, *tc.revision)
			}
			cos := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": GroupVersion.String(),
				"kind":       ClusterObjectSetKind,
				"metadata":   map[string]any{"name": name},
				"spec":       spec,
			}}
			err := c.Create(t.Context(), cos)
			if tc.valid {
				require.NoError(t, err)
				require.Len(t, group, 52)
				require.LessOrEqual(t, len(cos.GetName()), 63)
				return
			}
			require.True(t, errors.IsInvalid(err), "%v", err)
			require.ErrorContains(t, err, tc.expectedError)
		})
	}
}

func TestClusterObjectSetCanonicalName(t *testing.T) {
	c := newClient(t)
	for _, tc := range []struct {
		testName     string
		name         string
		generateName string
		group        string
		revision     int64
		valid        bool
	}{
		{testName: "canonical", name: "canonical-1", group: "canonical", revision: 1, valid: true},
		{testName: "different group", name: "other-group-1", group: "different-group", revision: 1, valid: false},
		{testName: "leading zero", name: "leading-zero-01", group: "leading-zero", revision: 1, valid: false},
		{testName: "wrong revision", name: "wrong-revision-2", group: "wrong-revision", revision: 1, valid: false},
		{testName: "explicit name takes precedence over generateName", name: "explicit-1", generateName: "unrelated-prefix-", group: "explicit", revision: 1, valid: true},
		// The prefix cannot produce the canonical name, regardless of the random suffix.
		{testName: "server generated name", generateName: "unrelated-prefix-", group: "generated", revision: 1, valid: false},
	} {
		t.Run(tc.testName, func(t *testing.T) {
			cos := &ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{Name: tc.name, GenerateName: tc.generateName},
				Spec: ClusterObjectSetSpec{
					Group:               tc.group,
					Revision:            tc.revision,
					LifecycleState:      ClusterObjectSetLifecycleStateActive,
					CollisionProtection: CollisionProtectionPrevent,
				},
			}
			err := c.Create(t.Context(), cos)
			if tc.valid {
				require.NoError(t, err)
				return
			}
			require.True(t, errors.IsInvalid(err), "%v", err)
			require.ErrorContains(t, err, "metadata.name must be <spec.group>-<spec.revision>")
		})
	}
}

func TestClusterObjectSetGroupValidation(t *testing.T) {
	c := newClient(t)
	for i, tc := range []struct {
		name          string
		group         *string
		valid         bool
		expectedError string
	}{
		{name: "missing group", expectedError: "spec.group: Required value"},
		{name: "empty group", group: ptr.To(""), expectedError: "spec.group: Invalid value"},
		{name: "one character", group: ptr.To("a"), valid: true},
		{name: "lowercase hyphens and digits", group: ptr.To("my-group-1"), valid: true},
		{name: "maximum length", group: ptr.To(strings.Repeat("a", 52)), valid: true},
		{name: "over maximum length", group: ptr.To(strings.Repeat("a", 53)), expectedError: "spec.group: Too long"},
		{name: "starts with digit", group: ptr.To("1group"), expectedError: "group must start with a lowercase letter"},
		{name: "starts with hyphen", group: ptr.To("-group"), expectedError: "group must start with a lowercase letter"},
		{name: "ends with hyphen", group: ptr.To("group-"), expectedError: "group must start with a lowercase letter"},
		{name: "uppercase", group: ptr.To("Group"), expectedError: "group must start with a lowercase letter"},
		{name: "underscore", group: ptr.To("my_group"), expectedError: "group must start with a lowercase letter"},
		{name: "dot", group: ptr.To("my.group"), expectedError: "group must start with a lowercase letter"},
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
				"revision":            int64(1),
				"lifecycleState":      string(ClusterObjectSetLifecycleStateActive),
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
				require.ErrorContains(t, err, tc.expectedError)
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
func probeSpec(group string, probes ...ProgressionProbe) ClusterObjectSetSpec {
	return ClusterObjectSetSpec{
		Group:               group,
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
func refSpec(group string, ref ObjectSourceRef) ClusterObjectSetSpec {
	return ClusterObjectSetSpec{
		Group:               group,
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

// TestObjectCountsSerializationIncludesZeroCounts guards against the count fields
// being dropped from status when zero. total/present/synced/ready are all required,
// so a "0 of N" count (e.g. ready=0 while a phase is rolling out, or synced/ready=0
// during teardown) must still be serialized rather than omitted via omitempty.
func TestObjectCountsSerializationIncludesZeroCounts(t *testing.T) {
	data, err := json.Marshal(ObjectCounts{Total: 1, Present: 1, Synced: 1, Ready: 0})
	require.NoError(t, err)
	require.JSONEq(t, `{"total":1,"present":1,"synced":1,"ready":0}`, string(data))

	// All-zero counts must still serialize every field (nothing omitted).
	data, err = json.Marshal(ObjectCounts{})
	require.NoError(t, err)
	require.JSONEq(t, `{"total":0,"present":0,"synced":0,"ready":0}`, string(data))
}

// TestObjectCountsOmittedWhenZero verifies the parent objectCounts field is optional:
// a zero-valued ObjectCounts is omitted entirely (via omitzero on a value struct —
// omitempty does not omit non-pointer structs), but when any count is non-zero the whole
// struct is serialized with all of its (required) fields, including zeros.
func TestObjectCountsOmittedWhenZero(t *testing.T) {
	// Aggregate status: zero objectCounts is omitted.
	data, err := json.Marshal(ClusterObjectSetStatus{})
	require.NoError(t, err)
	require.NotContains(t, string(data), "objectCounts")

	// Aggregate status: a non-zero objectCounts serializes with every field.
	data, err = json.Marshal(ClusterObjectSetStatus{ObjectCounts: ObjectCounts{Total: 1}})
	require.NoError(t, err)
	require.Contains(t, string(data), `"objectCounts":{"total":1,"present":0,"synced":0,"ready":0}`)

	// Per-phase: zero objectCounts is omitted (name/digest still present).
	data, err = json.Marshal(ObservedPhase{Name: "deploy", Digest: "sha256:abc"})
	require.NoError(t, err)
	require.NotContains(t, string(data), "objectCounts")

	// Per-phase: a non-zero objectCounts serializes with every field.
	data, err = json.Marshal(ObservedPhase{Name: "deploy", Digest: "sha256:abc", ObjectCounts: ObjectCounts{Present: 2}})
	require.NoError(t, err)
	require.Contains(t, string(data), `"objectCounts":{"total":0,"present":2,"synced":0,"ready":0}`)
}
