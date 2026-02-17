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

func TestClusterExtensionRevisionImmutability(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	i := 0
	for name, tc := range map[string]struct {
		spec       ClusterExtensionRevisionSpec
		updateFunc func(*ClusterExtensionRevision)
		allowed    bool
	}{
		"revision is immutable": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState:      ClusterExtensionRevisionLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cer *ClusterExtensionRevision) {
				cer.Spec.Revision = 2
			},
		},
		"phases may be initially empty": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState:      ClusterExtensionRevisionLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
				Phases:              []ClusterExtensionRevisionPhase{},
			},
			updateFunc: func(cer *ClusterExtensionRevision) {
				cer.Spec.Phases = []ClusterExtensionRevisionPhase{
					{
						Name:    "foo",
						Objects: []ClusterExtensionRevisionObject{},
					},
				}
			},
			allowed: true,
		},
		"phases may be initially unset": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState:      ClusterExtensionRevisionLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cer *ClusterExtensionRevision) {
				cer.Spec.Phases = []ClusterExtensionRevisionPhase{
					{
						Name:    "foo",
						Objects: []ClusterExtensionRevisionObject{},
					},
				}
			},
			allowed: true,
		},
		"phases are immutable if not empty": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState:      ClusterExtensionRevisionLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
				Phases: []ClusterExtensionRevisionPhase{
					{
						Name:    "foo",
						Objects: []ClusterExtensionRevisionObject{},
					},
				},
			},
			updateFunc: func(cer *ClusterExtensionRevision) {
				cer.Spec.Phases = []ClusterExtensionRevisionPhase{
					{
						Name:    "foo2",
						Objects: []ClusterExtensionRevisionObject{},
					},
				}
			},
		},
		"spec collisionProtection is immutable": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState:      ClusterExtensionRevisionLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			updateFunc: func(cer *ClusterExtensionRevision) {
				cer.Spec.CollisionProtection = CollisionProtectionNone
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			cer := &ClusterExtensionRevision{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("foo%d", i),
				},
				Spec: tc.spec,
			}
			i = i + 1
			require.NoError(t, c.Create(ctx, cer))
			tc.updateFunc(cer)
			err := c.Update(ctx, cer)
			if tc.allowed && err != nil {
				t.Fatal("expected update to succeed, but got:", err)
			}
			if !tc.allowed && !errors.IsInvalid(err) {
				t.Fatal("expected update to fail due to invalid payload, but got:", err)
			}
		})
	}
}

func TestClusterExtensionRevisionValidity(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	i := 0
	for name, tc := range map[string]struct {
		spec  ClusterExtensionRevisionSpec
		valid bool
	}{
		"revision cannot be negative": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState: ClusterExtensionRevisionLifecycleStateActive,
				Revision:       -1,
			},
			valid: false,
		},
		"revision cannot be zero": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState: ClusterExtensionRevisionLifecycleStateActive,
			},
			valid: false,
		},
		"revision must be positive": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState:      ClusterExtensionRevisionLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			valid: true,
		},
		"lifecycleState must be set": {
			spec: ClusterExtensionRevisionSpec{
				Revision: 1,
			},
			valid: false,
		},
		"phases must have no more than 20 phases": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState: ClusterExtensionRevisionLifecycleStateActive,
				Revision:       1,
				Phases:         make([]ClusterExtensionRevisionPhase, 21),
			},
			valid: false,
		},
		"phases entries must have no more than 50 objects": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState: ClusterExtensionRevisionLifecycleStateActive,
				Revision:       1,
				Phases: []ClusterExtensionRevisionPhase{
					{
						Name:    "too-many-objects",
						Objects: make([]ClusterExtensionRevisionObject, 51),
					},
				},
			},
			valid: false,
		},
		"phases entry names cannot be empty": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState: ClusterExtensionRevisionLifecycleStateActive,
				Revision:       1,
				Phases: []ClusterExtensionRevisionPhase{
					{
						Name: "",
					},
				},
			},
			valid: false,
		},
		"phases entry names cannot start with symbols": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState: ClusterExtensionRevisionLifecycleStateActive,
				Revision:       1,
				Phases: []ClusterExtensionRevisionPhase{
					{
						Name: "-invalid",
					},
				},
			},
			valid: false,
		},
		"phases entry names cannot start with numeric characters": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState: ClusterExtensionRevisionLifecycleStateActive,
				Revision:       1,
				Phases: []ClusterExtensionRevisionPhase{
					{
						Name: "1-invalid",
					},
				},
			},
			valid: false,
		},
		"spec collisionProtection accepts Prevent": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState:      ClusterExtensionRevisionLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
			},
			valid: true,
		},
		"spec collisionProtection accepts IfNoController": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState:      ClusterExtensionRevisionLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionIfNoController,
			},
			valid: true,
		},
		"spec collisionProtection accepts None": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState:      ClusterExtensionRevisionLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionNone,
			},
			valid: true,
		},
		"spec collisionProtection is required": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState: ClusterExtensionRevisionLifecycleStateActive,
				Revision:       1,
			},
			valid: false,
		},
		"spec collisionProtection rejects invalid values": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState:      ClusterExtensionRevisionLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtection("Invalid"),
			},
			valid: false,
		},
		"spec collisionProtection must be set": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState: ClusterExtensionRevisionLifecycleStateActive,
				Revision:       1,
			},
			valid: false,
		},
		"object collisionProtection is optional": {
			spec: ClusterExtensionRevisionSpec{
				LifecycleState:      ClusterExtensionRevisionLifecycleStateActive,
				Revision:            1,
				CollisionProtection: CollisionProtectionPrevent,
				Phases: []ClusterExtensionRevisionPhase{
					{
						Name: "deploy",
						Objects: []ClusterExtensionRevisionObject{
							{
								Object: configMap(),
							},
						},
					},
				},
			},
			valid: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			cer := &ClusterExtensionRevision{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("bar%d", i),
				},
				Spec: tc.spec,
			}
			i = i + 1
			err := c.Create(ctx, cer)
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
