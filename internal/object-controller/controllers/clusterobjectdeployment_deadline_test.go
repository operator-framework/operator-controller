//go:build !standard

package controllers_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clocktesting "k8s.io/utils/clock/testing"
	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/machinery"
	machinerytypes "pkg.package-operator.run/boxcutter/machinery/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/testutil"
)

func Test_ClusterObjectDeploymentReconciler_DeadlineCoversEntireRevision(t *testing.T) {
	creation := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	clk := clocktesting.NewFakeClock(creation)
	scheme := newScheme(t)
	require.NoError(t, corev1.AddToScheme(scheme))
	mockCtrl := gomock.NewController(t)

	cod := codFixture()
	cod.UID = types.UID(cod.Name)
	// Time before the COS exists is outside the revision's budget, even when
	// creating it took longer than the configured deadline.
	cod.CreationTimestamp = metav1.NewTime(creation.Add(-30 * time.Minute))
	cod.Spec.Template.Spec.ProgressDeadlineMinutes = 10
	phaseTemplate := cod.Spec.Template.Spec.Phases[0]
	cod.Spec.Template.Spec.Phases = nil
	for i := range 5 {
		phase := phaseTemplate.DeepCopy()
		phase.Name = fmt.Sprintf("phase-%d", i+1)
		phase.Objects[0].Object.SetName(phase.Name)
		cod.Spec.Template.Spec.Phases = append(cod.Spec.Template.Spec.Phases, *phase)
	}

	c := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), controllers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectDeployment{}, &ocv1.ClusterObjectSet{}).
		WithObjects(cod).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				// Supply the creation metadata normally assigned by the API server.
				obj.SetCreationTimestamp(metav1.NewTime(clk.Now()))
				obj.SetUID(types.UID(obj.GetName()))
				return cl.Create(ctx, obj, opts...)
			},
		}).Build()
	_, err := reconcileCOD(t, c)
	require.NoError(t, err)
	cosName := cod.Name + "-1"
	cos, found := getCOS(t, c, cosName)
	require.True(t, found)
	require.True(t, cos.CreationTimestamp.Time.Equal(creation))
	require.Equal(t, int32(10), cos.Spec.ProgressDeadlineMinutes)
	require.Len(t, cos.Spec.Phases, 5)
	require.Equal(t, metav1.ConditionTrue, progressing(t, c).Status)
	// Start after finalizer initialization; the fake client's metadata patch
	// otherwise drops the first reconcile's in-memory phase digests.
	cos.Finalizers = []string{"olm.operatorframework.io/teardown"}
	require.NoError(t, c.Update(t.Context(), cos))

	completedPhases := 0
	engine := newMockRevisionEngineWithReconcile(mockCtrl,
		func(context.Context, machinerytypes.Revision, ...machinerytypes.RevisionReconcileOption) (machinery.RevisionResult, error) {
			phases := make([]machinery.PhaseResult, 0, len(cos.Spec.Phases))
			for i, phase := range cos.Spec.Phases {
				probeStatus := machinerytypes.ProbeStatusFalse
				if i < completedPhases {
					probeStatus = machinerytypes.ProbeStatusTrue
				}
				phases = append(phases, newMockPhaseResult(mockCtrl, phaseResultConfig{
					name:       phase.Name,
					isComplete: i < completedPhases,
					objects: []machinery.ObjectResult{newMockObjectResult(mockCtrl, objectResultConfig{
						action:   machinery.ActionCreated,
						object:   phase.Objects[0].Object.DeepCopy(),
						complete: i < completedPhases,
						paused:   i > completedPhases,
						probes: machinerytypes.ProbeResultContainer{
							boxcutter.ProgressProbeType: {Status: probeStatus},
						},
					})},
				}))
			}
			return newMockRevisionResult(mockCtrl, revisionResultConfig{
				phases:     phases,
				isComplete: completedPhases == len(cos.Spec.Phases),
			}), nil
		}, nil,
	)
	cache := newMockTrackingCache(mockCtrl, c, nil)
	factory := newMockRevisionEngineFactoryWithEngine(mockCtrl, engine, nil)
	reconcileRevision := func() ctrl.Result {
		t.Helper()
		// Reconstruct the reconciler each time so timing also survives restarts.
		r := &controllers.ClusterObjectSetReconciler{
			Client: c, Clock: clk, TrackingCache: cache, RevisionEngineFactory: factory,
		}
		result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: types.NamespacedName{Name: cosName}})
		require.NoError(t, err)
		_, err = reconcileCOD(t, c)
		require.NoError(t, err)
		return result
	}

	// Four phase completions consume eight minutes of the same ten-minute budget.
	for completedPhases = 0; completedPhases < 5; completedPhases++ {
		elapsed := time.Duration(completedPhases*2) * time.Minute
		clk.SetTime(creation.Add(elapsed))
		result := reconcileRevision()
		cos, found = getCOS(t, c, cosName)
		require.True(t, found)
		require.Equal(t, 10*time.Minute-elapsed, result.RequeueAfter, "revision conditions: %+v", cos.Status.Conditions)
		require.EqualValues(t, completedPhases, cos.Status.ObjectCounts.Ready)
		require.True(t, cos.Status.CompletedAt.IsZero())
		condition := progressing(t, c)
		require.NotNil(t, condition)
		require.Equal(t, metav1.ConditionTrue, condition.Status)
		require.Equal(t, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, condition.Reason)
	}

	// The last phase remains incomplete when the revision reaches its deadline.
	completedPhases = 4
	clk.SetTime(creation.Add(10 * time.Minute))
	require.Zero(t, reconcileRevision().RequeueAfter)
	cos, found = getCOS(t, c, cosName)
	require.True(t, found)
	ready := apimeta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
	require.NotNil(t, ready)
	require.Equal(t, metav1.ConditionFalse, ready.Status)
	require.Equal(t, ocv1.ReasonProgressDeadlineExceeded, ready.Reason)
	condition := progressing(t, c)
	require.NotNil(t, condition)
	require.Equal(t, metav1.ConditionFalse, condition.Status)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded, condition.Reason)

	// Completing the revision still recovers after timeout.
	completedPhases = 5
	clk.Step(time.Minute)
	require.Zero(t, reconcileRevision().RequeueAfter)
	cos, found = getCOS(t, c, cosName)
	require.True(t, found)
	require.True(t, cos.Status.CompletedAt.Time.Equal(clk.Now()))
	condition = progressing(t, c)
	require.NotNil(t, condition)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, condition.Reason)

	// Later health regressions do not make the completed revision time out again.
	completedPhases = 4
	clk.Step(30 * time.Minute)
	require.Zero(t, reconcileRevision().RequeueAfter)
	cos, found = getCOS(t, c, cosName)
	require.True(t, found)
	ready = apimeta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeReady)
	require.NotNil(t, ready)
	require.Equal(t, ocv1.ClusterObjectSetReasonObjectsNotReady, ready.Reason)
	condition = progressing(t, c)
	require.NotNil(t, condition)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, condition.Reason)

	// A later revision receives a fresh budget despite the deployment's age and
	// the previous revision's earlier timeout.
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(cod), cod))
	cod.Spec.Template.Spec.Phases[0].Objects[0].Object.Object["data"] = map[string]interface{}{"version": "second"}
	require.NoError(t, c.Update(t.Context(), cod))
	_, err = reconcileCOD(t, c)
	require.NoError(t, err)
	cosName = cod.Name + "-2"
	cos, found = getCOS(t, c, cosName)
	require.True(t, found)
	require.True(t, cos.CreationTimestamp.Time.Equal(clk.Now()))
	cos.Finalizers = []string{"olm.operatorframework.io/teardown"}
	require.NoError(t, c.Update(t.Context(), cos))
	completedPhases = 0
	require.Equal(t, 10*time.Minute, reconcileRevision().RequeueAfter)
	require.Equal(t, metav1.ConditionTrue, progressing(t, c).Status)
}
