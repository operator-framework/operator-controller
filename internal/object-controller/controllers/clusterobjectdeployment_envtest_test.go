//go:build !standard

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/object-controller/controllers"
)

// TestClusterObjectDeploymentReconciler_RevisionOwnership_Envtest verifies the COD
// adoption behavior against a real API server: adopting orphan COSes and reporting
// conflicts on foreign-owned COSes.
func TestClusterObjectDeploymentReconciler_RevisionOwnership_Envtest(t *testing.T) {
	// Use a unique group name for this test to avoid conflicts
	groupName := fmt.Sprintf("envtest-ownership-%d", time.Now().UnixNano())

	// Set up the manager with cache and index
	scheme := newScheme(t)
	mgr, err := manager.New(config, manager.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		HealthProbeBindAddress: "0",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Register the .spec.group field index before starting the cache
	require.NoError(t, controllers.SetupIndexes(ctx, mgr.GetFieldIndexer()))

	// Start the manager in a goroutine
	go func() {
		if err := mgr.Start(ctx); err != nil {
			t.Logf("manager stopped with error: %v", err)
		}
	}()

	// Wait for cache to sync
	require.True(t, mgr.GetCache().WaitForCacheSync(ctx))

	cl := mgr.GetClient()

	// Create the ClusterObjectDeployment
	cod := &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: groupName},
		Spec: ocv1.ClusterObjectDeploymentSpec{
			Template: ocv1.ClusterObjectDeploymentTemplate{
				Spec: ocv1.ClusterObjectDeploymentTemplateSpec{
					CollisionProtection:     ocv1.CollisionProtectionPrevent,
					ProgressDeadlineMinutes: 15,
					Phases: []ocv1.ClusterObjectSetPhase{{
						Name: "resources",
						Objects: []ocv1.ClusterObjectSetObject{{
							Object: unstructured.Unstructured{Object: map[string]interface{}{
								"apiVersion": "v1",
								"kind":       "ConfigMap",
								"metadata":   map[string]interface{}{"name": "test-cm", "namespace": "default"},
							}},
						}},
					}},
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, cod))
	t.Cleanup(func() {
		require.NoError(t, cl.Delete(context.Background(), cod))
	})

	// Create an orphan COS (no controller owner)
	orphanCOS := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-1", groupName)},
		Spec: ocv1.ClusterObjectSetSpec{
			Group:               groupName,
			Revision:            1,
			CollisionProtection: ocv1.CollisionProtectionPrevent,
			LifecycleState:      ocv1.ClusterObjectSetLifecycleStateActive,
		},
	}
	require.NoError(t, cl.Create(ctx, orphanCOS))
	t.Cleanup(func() {
		require.NoError(t, cl.Delete(context.Background(), orphanCOS))
	})

	// Create a foreign-owned COS (owned by a different controller)
	foreignOwnerRef := metav1.OwnerReference{
		APIVersion:         ocv1.GroupVersion.String(),
		Kind:               ocv1.ClusterObjectDeploymentKind,
		Name:               "foreign-owner",
		UID:                "foreign-uid-12345",
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
	foreignOwnedCOS := &ocv1.ClusterObjectSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("%s-2", groupName),
			OwnerReferences: []metav1.OwnerReference{foreignOwnerRef},
		},
		Spec: ocv1.ClusterObjectSetSpec{
			Group:               groupName,
			Revision:            2,
			CollisionProtection: ocv1.CollisionProtectionPrevent,
			LifecycleState:      ocv1.ClusterObjectSetLifecycleStateActive,
		},
	}
	require.NoError(t, cl.Create(ctx, foreignOwnedCOS))
	t.Cleanup(func() {
		require.NoError(t, cl.Delete(context.Background(), foreignOwnedCOS))
	})

	// Create the reconciler
	reconciler := &controllers.ClusterObjectDeploymentReconciler{
		Client: cl,
		Scheme: mgr.GetScheme(),
	}

	// Reconcile and assert within EventuallyWithT to handle cache consistency
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{Name: groupName},
		})
		if !assert.NoError(collect, err) {
			return
		}

		// Re-fetch the COD to get its UID for ownership verification
		reconciledCOD := &ocv1.ClusterObjectDeployment{}
		if !assert.NoError(collect, cl.Get(ctx, types.NamespacedName{Name: groupName}, reconciledCOD)) {
			return
		}

		// Re-fetch the orphan COS and verify it's now owned by the COD
		adoptedOrphan := &ocv1.ClusterObjectSet{}
		if !assert.NoError(collect, cl.Get(ctx, types.NamespacedName{Name: orphanCOS.Name}, adoptedOrphan)) {
			return
		}
		owner := metav1.GetControllerOf(adoptedOrphan)
		if !assert.NotNil(collect, owner, "orphan COS should now have a controller owner") {
			return
		}
		assert.Equal(collect, ocv1.ClusterObjectDeploymentKind, owner.Kind)
		assert.Equal(collect, groupName, owner.Name)
		assert.Equal(collect, reconciledCOD.UID, owner.UID, "orphan COS should be owned by this COD")

		// Re-fetch the foreign-owned COS and verify it's unchanged
		unchangedForeign := &ocv1.ClusterObjectSet{}
		if !assert.NoError(collect, cl.Get(ctx, types.NamespacedName{Name: foreignOwnedCOS.Name}, unchangedForeign)) {
			return
		}
		foreignOwner := metav1.GetControllerOf(unchangedForeign)
		if !assert.NotNil(collect, foreignOwner, "foreign-owned COS should still have its original owner") {
			return
		}
		assert.Equal(collect, foreignOwnerRef.Name, foreignOwner.Name)
		assert.Equal(collect, foreignOwnerRef.UID, foreignOwner.UID)

		// Verify the COD reports RevisionOwnershipConflict
		progressingCond := apimeta.FindStatusCondition(reconciledCOD.Status.Conditions, ocv1.ClusterObjectDeploymentTypeProgressing)
		if !assert.NotNil(collect, progressingCond, "COD should have Progressing condition") {
			return
		}
		assert.Equal(collect, metav1.ConditionFalse, progressingCond.Status)
		assert.Equal(collect, ocv1.ClusterObjectDeploymentReasonRevisionOwnershipConflict, progressingCond.Reason)
		assert.Contains(collect, progressingCond.Message, fmt.Sprintf("%s-2", groupName))
	}, 10*time.Second, 100*time.Millisecond, "COD should adopt orphan and report conflict on foreign-owned COS")
}
