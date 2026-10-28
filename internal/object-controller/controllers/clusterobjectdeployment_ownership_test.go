//go:build !standard

package controllers_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/object-controller/controllers"
)

// TestClusterObjectDeploymentGroupEvents runs the COD controller against envtest
// to exercise its real group watch. COS status is supplied as input to isolate
// orchestration from the COS controller's independent revision lifecycle. Owned
// and foreign revisions describe disjoint objects so ownership handover cannot
// account for changes in availability.
func TestClusterObjectDeploymentGroupEvents(t *testing.T) {
	cl := newCODManagerClient(t)
	for _, resolution := range []string{"archival", "orphaning", "deletion"} {
		t.Run(resolution, func(t *testing.T) {
			cod := codFixture()
			cod.Name = "cod-group-events-" + resolution
			cod.UID = ""
			cod.Spec.Template.Spec.Phases[0].Objects[0].Object.SetName("owned-" + resolution)
			require.NoError(t, cl.Create(t.Context(), cod))
			t.Cleanup(func() {
				ctx := context.Background()
				require.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, cod)))
				var revisions ocv1.ClusterObjectSetList
				require.NoError(t, cl.List(ctx, &revisions))
				for i := range revisions.Items {
					if revisions.Items[i].Spec.Group == cod.Name {
						require.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, &revisions.Items[i])))
					}
				}
			})

			owned := &ocv1.ClusterObjectSet{}
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				assert.NoError(collect, cl.Get(t.Context(), client.ObjectKey{Name: cod.Name + "-1"}, owned))
			}, 10*time.Second, 10*time.Millisecond)
			owned.Status.CompletedAt = metav1.Now()
			owned.Status.ObjectCounts = ocv1.ObjectCounts{Total: 1, Present: 1, Synced: 1, Ready: 1}
			setCOSReady(owned, metav1.ConditionTrue, ocv1.ClusterObjectSetReasonAllObjectsReady)
			require.NoError(t, cl.Status().Update(t.Context(), owned))
			waitForCODCondition(t, cl, cod.Name, ocv1.ClusterObjectDeploymentTypeAvailable,
				metav1.ConditionTrue, ocv1.ClusterObjectDeploymentReasonAvailable, 0)

			foreign := owned.DeepCopy()
			foreign.ObjectMeta = metav1.ObjectMeta{
				Name: cod.Name + "-3", Labels: owned.Labels,
				OwnerReferences: []metav1.OwnerReference{foreignControllerReference()},
			}
			foreign.Spec.Revision = 3
			foreign.Spec.Phases[0].Objects[0].Object.SetName("foreign-" + resolution)
			foreign.Status = ocv1.ClusterObjectSetStatus{}
			require.NoError(t, cl.Create(t.Context(), foreign))
			waitForCODCondition(t, cl, cod.Name, ocv1.ClusterObjectDeploymentTypeProgressing,
				metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonOwnerConflict, 0)

			// A ready foreign revision cannot supply the COD's availability.
			setCOSReady(foreign, metav1.ConditionTrue, ocv1.ClusterObjectSetReasonAllObjectsReady)
			foreign.Status.ObjectCounts = ocv1.ObjectCounts{Total: 1, Present: 1, Synced: 1, Ready: 1}
			require.NoError(t, cl.Status().Update(t.Context(), foreign))
			markCOSReady(t, cl, owned.Name, false)
			waitForCODCondition(t, cl, cod.Name, ocv1.ClusterObjectDeploymentTypeAvailable,
				metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonUnavailable, 0)
			markCOSReady(t, cl, owned.Name, true)
			waitForCODCondition(t, cl, cod.Name, ocv1.ClusterObjectDeploymentTypeAvailable,
				metav1.ConditionTrue, ocv1.ClusterObjectDeploymentReasonAvailable, 0)

			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(cod), cod))
			cod.Spec.Template.Spec.CollisionProtection = ocv1.CollisionProtectionNone
			require.NoError(t, cl.Update(t.Context(), cod))
			waitForCODCondition(t, cl, cod.Name, ocv1.ClusterObjectDeploymentTypeProgressing,
				metav1.ConditionFalse, ocv1.ClusterObjectDeploymentReasonOwnerConflict, cod.Generation)
			require.True(t, apierrors.IsNotFound(cl.Get(t.Context(), client.ObjectKey{Name: cod.Name + "-4"}, &ocv1.ClusterObjectSet{})))
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(owned), owned))
			require.Equal(t, ocv1.ObjectCounts{Total: 1, Present: 1, Synced: 1, Ready: 1}, owned.Status.ObjectCounts)

			// Only the foreign COS changes here. Its group event must release the
			// conflict and reconcile the already-updated COD template.
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(foreign), foreign))
			expectedRevision := int64(4)
			switch resolution {
			case "archival":
				foreign.Spec.LifecycleState = ocv1.ClusterObjectSetLifecycleStateArchived
				require.NoError(t, cl.Update(t.Context(), foreign))
			case "orphaning":
				foreign.OwnerReferences = nil
				require.NoError(t, cl.Update(t.Context(), foreign))
			case "deletion":
				require.NoError(t, cl.Delete(t.Context(), foreign))
				expectedRevision = 2
			}
			created := &ocv1.ClusterObjectSet{}
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				if assert.NoError(collect, cl.Get(t.Context(), client.ObjectKey{Name: fmt.Sprintf("%s-%d", cod.Name, expectedRevision)}, created)) {
					assert.True(collect, metav1.IsControlledBy(created, cod))
				}
			}, 10*time.Second, 10*time.Millisecond)
			waitForCODCondition(t, cl, cod.Name, ocv1.ClusterObjectDeploymentTypeProgressing,
				metav1.ConditionTrue, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, cod.Generation)
			waitForCODCondition(t, cl, cod.Name, ocv1.ClusterObjectDeploymentTypeAvailable,
				metav1.ConditionTrue, ocv1.ClusterObjectDeploymentReasonAvailable, cod.Generation)
			if resolution != "deletion" {
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(foreign), foreign))
				if resolution == "orphaning" {
					require.True(t, metav1.IsControlledBy(foreign, cod))
				} else {
					require.Equal(t, foreignControllerReference(), *metav1.GetControllerOf(foreign))
				}
			}
		})
	}
}

func newCODManagerClient(t *testing.T) client.Client {
	t.Helper()
	scheme := newScheme(t)
	cl, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)
	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme: scheme, Logger: logr.Discard(),
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		Controller:             ctrlconfig.Controller{SkipNameValidation: ptr.To(true)},
	})
	require.NoError(t, err)
	require.NoError(t, controllers.SetupIndexes(t.Context(), mgr.GetFieldIndexer()))
	require.NoError(t, (&controllers.ClusterObjectDeploymentReconciler{
		Client: mgr.GetClient(), Scheme: scheme,
	}).SetupWithManager(mgr))
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- mgr.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Fatal("COD manager did not stop")
		}
	})
	syncCtx, syncCancel := context.WithTimeout(ctx, 10*time.Second)
	defer syncCancel()
	require.True(t, mgr.GetCache().WaitForCacheSync(syncCtx))
	return cl
}

func waitForCODCondition(t *testing.T, cl client.Client, name, conditionType string, status metav1.ConditionStatus, reason string, generation int64) {
	t.Helper()
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		cod := &ocv1.ClusterObjectDeployment{}
		if !assert.NoError(collect, cl.Get(t.Context(), client.ObjectKey{Name: name}, cod)) {
			return
		}
		condition := apimeta.FindStatusCondition(cod.Status.Conditions, conditionType)
		if !assert.NotNil(collect, condition) {
			return
		}
		assert.Equal(collect, status, condition.Status)
		assert.Equal(collect, reason, condition.Reason)
		if generation != 0 {
			assert.Equal(collect, generation, condition.ObservedGeneration)
		}
	}, 10*time.Second, 10*time.Millisecond)
}
