package main

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

func TestClusterObjectDeploymentCache(t *testing.T) {
	t.Parallel()
	adoptionLabels := map[string]string{
		labels.ManagedByKey: labels.ManagedByOperatorController,
	}
	wantAnnotations := map[string]string{"olm.operatorframework.io/content-hash": "keep-me"}
	type testCase struct {
		name            string
		codLabels       map[string]string
		owner           *metav1.OwnerReference
		wantRolloutData bool
	}
	tests := []testCase{
		{name: "unlabelled ownerless deployment stays visible", wantRolloutData: false},
		{
			name: "migration staging reference retains rollout data",
			owner: &metav1.OwnerReference{
				APIVersion: ocv1.GroupVersion.String(), Kind: ocv1.ClusterExtensionKind,
				Name: "test-ext", UID: "test-uid",
			},
			wantRolloutData: true,
		},
		{name: "managed-by label alone retains rollout data", codLabels: adoptionLabels, wantRolloutData: true},
		{
			name: "mismatched owner labels do not prevent retaining rollout data",
			codLabels: map[string]string{
				labels.ManagedByKey: labels.ManagedByOperatorController,
				labels.OwnerKindKey: "Namespace",
				labels.OwnerNameKey: "other-ext",
			},
			wantRolloutData: true,
		},
		{
			name: "owner labels alone do not retain rollout data",
			codLabels: map[string]string{
				labels.OwnerKindKey: ocv1.ClusterExtensionKind,
				labels.OwnerNameKey: "test-ext",
			},
			wantRolloutData: false,
		},
		{
			name:            "different managed-by value does not retain rollout data",
			codLabels:       map[string]string{labels.ManagedByKey: "object-controller"},
			wantRolloutData: false,
		},
		{
			name: "matching controller without managed-by label does not retain rollout data",
			owner: &metav1.OwnerReference{
				APIVersion: ocv1.GroupVersion.String(), Kind: ocv1.ClusterExtensionKind,
				Name: "test-ext", UID: "test-uid", Controller: ptr.To(true),
			},
			wantRolloutData: false,
		},
		{
			name: "foreign controller metadata stays visible",
			owner: &metav1.OwnerReference{
				APIVersion: "v1", Kind: "Namespace", Name: "test-ext",
				UID: "foreign-uid", Controller: ptr.To(true),
			},
			wantRolloutData: false,
		},
		{
			name:      "managed-by label retains rollout data regardless of controller",
			codLabels: adoptionLabels,
			owner: &metav1.OwnerReference{
				APIVersion: "v1", Kind: "Namespace", Name: "test-ext",
				UID: "foreign-uid", Controller: ptr.To(true),
			},
			wantRolloutData: true,
		},
	}
	options := newCacheOptions("olmv1-system", true)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cod := &ocv1.ClusterObjectDeployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ext", UID: "cod-uid", Labels: maps.Clone(tc.codLabels),
					Annotations: map[string]string{
						"kubectl.kubernetes.io/last-applied-configuration": "large manifest",
						"olm.operatorframework.io/content-hash":            "keep-me",
					},
				},
				Spec: ocv1.ClusterObjectDeploymentSpec{RevisionHistoryLimit: ptr.To(int32(5))},
				Status: ocv1.ClusterObjectDeploymentStatus{Conditions: []metav1.Condition{{
					Type: ocv1.ClusterObjectDeploymentTypeAvailable, Status: metav1.ConditionTrue,
				}}},
			}
			if tc.owner != nil {
				cod.OwnerReferences = []metav1.OwnerReference{*tc.owner}
			}
			before := cod.DeepCopy()
			for obj, config := range options.ByObject {
				if _, ok := obj.(*ocv1.ClusterObjectDeployment); !ok {
					continue
				}
				require.NotNil(t, config.Label)
				assert.True(t, config.Label.Matches(k8slabels.Set(cod.Labels)), "name-based reads must see every COD")
				require.NotNil(t, config.Transform)
				transformed, err := config.Transform(cod)
				require.NoError(t, err)
				actual, ok := transformed.(*ocv1.ClusterObjectDeployment)
				require.True(t, ok)
				assert.Equal(t, before.Labels, actual.Labels)
				assert.Equal(t, before.OwnerReferences, actual.OwnerReferences)
				assert.Equal(t, before.Name, actual.Name)
				assert.Equal(t, before.UID, actual.UID)
				assert.Equal(t, wantAnnotations, actual.Annotations)
				if tc.wantRolloutData {
					assert.Equal(t, before.Spec, actual.Spec)
					assert.Equal(t, before.Status, actual.Status)
				} else {
					assert.Empty(t, actual.Spec)
					assert.Empty(t, actual.Status)
				}
				return
			}
			t.Fatal("COD cache configuration is missing")
		})
	}
}
