package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestClusterObjectSetImmutability(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	i := 0
	for name, tc := range map[string]struct {
		spec       ClusterObjectSetSpec
		updateFunc func(*ClusterObjectSet)
		allowed    bool
	}{
		"revision is immutable": {
			spec: ClusterObjectSetSpec{
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
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cos *ClusterObjectSet) {
				cos.Spec.CollisionProtection = CollisionProtectionNone
			},
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
				LifecycleState: ClusterObjectSetLifecycleStateActive,
				Revision:       -1,
			},
			valid: false,
		},
		"revision cannot be zero": {
			spec: ClusterObjectSetSpec{
				LifecycleState: ClusterObjectSetLifecycleStateActive,
			},
			valid: false,
		},
		"revision must be positive": {
			spec: ClusterObjectSetSpec{
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			valid: true,
		},
		"lifecycleState must be set": {
			spec: ClusterObjectSetSpec{
				Revision: 1,
			},
			valid: false,
		},
		"phases must have no more than 20 phases": {
			spec: ClusterObjectSetSpec{
				LifecycleState: ClusterObjectSetLifecycleStateActive,
				Revision:       1,
				Phases:         make([]ClusterObjectSetPhase, 21),
			},
			valid: false,
		},
		"phases entries must have no more than 50 objects": {
			spec: ClusterObjectSetSpec{
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
		"spec collisionProtection accepts Prevent": {
			spec: ClusterObjectSetSpec{
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			valid: true,
		},
		"spec collisionProtection accepts IfNoController": {
			spec: ClusterObjectSetSpec{
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionIfNoController,
			},
			valid: true,
		},
		"spec collisionProtection accepts None": {
			spec: ClusterObjectSetSpec{
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionNone,
			},
			valid: true,
		},
		"spec collisionProtection is required": {
			spec: ClusterObjectSetSpec{
				LifecycleState: ClusterObjectSetLifecycleStateActive,
				Revision:       1,
			},
			valid: false,
		},
		"spec collisionProtection rejects invalid values": {
			spec: ClusterObjectSetSpec{
				LifecycleState:      ClusterObjectSetLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtection("Invalid"),
			},
			valid: false,
		},
		"spec collisionProtection must be set": {
			spec: ClusterObjectSetSpec{
				LifecycleState: ClusterObjectSetLifecycleStateActive,
				Revision:       1,
			},
			valid: false,
		},
		"object collisionProtection is optional": {
			spec: ClusterObjectSetSpec{
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
		"object with both object and ref is invalid": {
			spec: ClusterObjectSetSpec{
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
	} {
		t.Run(name, func(t *testing.T) {
			cos := &ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("bar%d", i),
				},
				Spec: tc.spec,
			}
			i = i + 1
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
