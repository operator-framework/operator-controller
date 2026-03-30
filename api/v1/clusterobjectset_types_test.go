package v1

import (
	"context"
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
