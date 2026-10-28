//go:build !standard

package controllers_test

import (
	"context"
	"errors"
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

func newDeadlineDeploymentClient(t *testing.T, cod *ocv1.ClusterObjectDeployment, clk *clocktesting.FakeClock) client.WithWatch {
	t.Helper()
	scheme := newScheme(t)
	require.NoError(t, corev1.AddToScheme(scheme))
	return testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(scheme), controllers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectDeployment{}, &ocv1.ClusterObjectSet{}).
		WithObjects(cod).
		WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
				obj.SetCreationTimestamp(metav1.NewTime(clk.Now()))
				obj.SetUID(types.UID(obj.GetName()))
				return cl.Create(ctx, obj, opts...)
			},
		}).Build()
}

func reconcileDeadlineDeployment(t *testing.T, c client.Client, clk *clocktesting.FakeClock) (ctrl.Result, error) {
	t.Helper()
	// Reconstruct the reconciler so timing cannot depend on process-local state.
	r := &controllers.ClusterObjectDeploymentReconciler{Client: c, Scheme: newScheme(t), Clock: clk}
	return r.Reconcile(t.Context(), ctrl.Request{NamespacedName: types.NamespacedName{Name: clusterObjectDeploymentName}})
}

func TestClusterObjectDeploymentReconcilerDeadlineCoversEntireRevision(t *testing.T) {
	creation := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	clk := clocktesting.NewFakeClock(creation)
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

	c := newDeadlineDeploymentClient(t, cod, clk)
	result, err := reconcileDeadlineDeployment(t, c, clk)
	require.NoError(t, err)
	require.Equal(t, 10*time.Minute, result.RequeueAfter)
	cosName := cod.Name + "-1"
	cos, found := getCOS(t, c, cosName)
	require.True(t, found)
	require.True(t, cos.CreationTimestamp.Time.Equal(creation))
	require.Zero(t, cos.Spec.ProgressDeadlineMinutes, "the COD owns the deadline budget")
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
		deploymentResult, err := reconcileDeadlineDeployment(t, c, clk)
		require.NoError(t, err)
		require.Zero(t, result.RequeueAfter, "the COS does not schedule deadline checks")
		return deploymentResult
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
	require.Equal(t, ocv1.ClusterObjectSetReasonObjectsNotReady, ready.Reason)
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
	_, err = reconcileDeadlineDeployment(t, c, clk)
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

func TestClusterObjectDeploymentReconcilerDeadlineWithoutCOSDeadlineCondition(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status metav1.ConditionStatus
		reason string
		// wantReason is the expected COD Progressing reason. Empty means the deadline
		// governs the outcome (ProgressDeadlineExceeded); a hard-stall revision reason
		// short-circuits the deadline and is named explicitly.
		wantReason string
	}{
		{name: "no Ready condition"},
		{name: "pending", status: metav1.ConditionUnknown, reason: ocv1.ClusterObjectSetReasonPending},
		{name: "preconditions not met", status: metav1.ConditionFalse, reason: ocv1.ClusterObjectSetReasonPreconditionsNotMet},
		{name: "invalid specification", status: metav1.ConditionFalse, reason: ocv1.ClusterObjectSetReasonInvalidSpecification, wantReason: ocv1.ClusterObjectDeploymentReasonRevisionInvalidSpecification},
		{name: "object collision", status: metav1.ConditionFalse, reason: ocv1.ClusterObjectSetReasonObjectCollision, wantReason: ocv1.ClusterObjectDeploymentReasonRevisionObjectCollision},
		{name: "objects not ready", status: metav1.ConditionFalse, reason: ocv1.ClusterObjectSetReasonObjectsNotReady},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cod := codFixture()
			cod.Spec.Template.Spec.ProgressDeadlineMinutes = 10
			c := newCODClient(t, cod)
			_, err := reconcileCOD(t, c)
			require.NoError(t, err)
			cos, found := getCOS(t, c, cod.Name+"-1")
			require.True(t, found)
			cos.CreationTimestamp = metav1.NewTime(time.Now().Add(-11 * time.Minute))
			require.NoError(t, c.Update(t.Context(), cos))
			if tc.reason != "" {
				cos.Status.Conditions = []metav1.Condition{{
					Type: ocv1.ClusterObjectSetTypeReady, Status: tc.status, Reason: tc.reason,
				}}
				require.NoError(t, c.Status().Update(t.Context(), cos))
			}

			// Only reconcile the COD: it must enforce the revision deadline even
			// when the COS has not reported its own ProgressDeadlineExceeded reason.
			result, err := reconcileCOD(t, c)
			require.NoError(t, err)
			require.Zero(t, result.RequeueAfter)
			condition := progressing(t, c)
			require.NotNil(t, condition)
			require.Equal(t, metav1.ConditionFalse, condition.Status)
			wantReason := tc.wantReason
			if wantReason == "" {
				wantReason = ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded
			}
			require.Equal(t, wantReason, condition.Reason)
			cos, found = getCOS(t, c, cos.Name)
			require.True(t, found)
			if tc.reason != "" {
				require.Equal(t, tc.reason, apimeta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeReady).Reason)
			}
		})
	}
}

func TestClusterObjectDeploymentReconcilerIgnoresCOSDeadlineCondition(t *testing.T) {
	for _, deadline := range []int32{0, 10} {
		t.Run(fmt.Sprintf("deadline %d", deadline), func(t *testing.T) {
			clk := clocktesting.NewFakeClock(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
			cod := codFixture()
			cod.Spec.Template.Spec.ProgressDeadlineMinutes = deadline
			c := newDeadlineDeploymentClient(t, cod, clk)
			_, err := reconcileDeadlineDeployment(t, c, clk)
			require.NoError(t, err)
			cos, found := getCOS(t, c, cod.Name+"-1")
			require.True(t, found)
			cos.Status.Conditions = []metav1.Condition{{
				Type: ocv1.ClusterObjectSetTypeReady, Status: metav1.ConditionFalse,
				Reason: ocv1.ReasonProgressDeadlineExceeded,
			}}
			require.NoError(t, c.Status().Update(t.Context(), cos))

			result, err := reconcileDeadlineDeployment(t, c, clk)
			require.NoError(t, err)
			require.Equal(t, time.Duration(deadline)*time.Minute, result.RequeueAfter)
			condition := progressing(t, c)
			require.Equal(t, metav1.ConditionTrue, condition.Status)
			require.Equal(t, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, condition.Reason)
		})
	}
}

func TestClusterObjectDeploymentReconcilerDeadlineSchedulingAndRecovery(t *testing.T) {
	start := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	clk := clocktesting.NewFakeClock(start)
	cod := codFixture()
	cod.Spec.Template.Spec.ProgressDeadlineMinutes = 10
	c := newDeadlineDeploymentClient(t, cod, clk)
	result, err := reconcileDeadlineDeployment(t, c, clk)
	require.NoError(t, err)
	require.Equal(t, 10*time.Minute, result.RequeueAfter)
	cos, found := getCOS(t, c, cod.Name+"-1")
	require.True(t, found)
	cos.Status.Conditions = []metav1.Condition{{
		Type: ocv1.ClusterObjectSetTypeReady, Status: metav1.ConditionFalse,
		Reason: ocv1.ClusterObjectSetReasonPreconditionsNotMet, Message: "referenced Secret is mutable",
	}}
	require.NoError(t, c.Status().Update(t.Context(), cos))

	// A quiet COS need not report ProgressDeadlineExceeded for the COD to expire.
	clk.Step(9 * time.Minute)
	result, err = reconcileDeadlineDeployment(t, c, clk)
	require.NoError(t, err)
	require.Equal(t, time.Minute, result.RequeueAfter)
	require.Equal(t, metav1.ConditionTrue, progressing(t, c).Status)
	clk.Step(time.Minute)
	result, err = reconcileDeadlineDeployment(t, c, clk)
	require.NoError(t, err)
	require.Zero(t, result.RequeueAfter)
	condition := progressing(t, c)
	require.Equal(t, metav1.ConditionFalse, condition.Status)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded, condition.Reason)
	transition := condition.LastTransitionTime

	// Partial phase progress neither extends the budget nor clears the timeout.
	clk.Step(time.Minute)
	cos.Status.ObservedPhases = []ocv1.ObservedPhase{{Name: "resources", ObjectCounts: ocv1.ObjectCounts{Total: 1, Ready: 1}}}
	require.NoError(t, c.Status().Update(t.Context(), cos))
	result, err = reconcileDeadlineDeployment(t, c, clk)
	require.NoError(t, err)
	require.Zero(t, result.RequeueAfter)
	require.Equal(t, transition, progressing(t, c).LastTransitionTime)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded, progressing(t, c).Reason)
	cos, found = getCOS(t, c, cos.Name)
	require.True(t, found)
	require.Equal(t, ocv1.ClusterObjectSetReasonPreconditionsNotMet,
		apimeta.FindStatusCondition(cos.Status.Conditions, ocv1.ClusterObjectSetTypeReady).Reason)

	// The historical completion latch wins even over a stale COS deadline reason.
	cos.Status.CompletedAt = metav1.NewTime(clk.Now())
	cos.Status.Conditions[0].Reason = ocv1.ReasonProgressDeadlineExceeded
	require.NoError(t, c.Status().Update(t.Context(), cos))
	result, err = reconcileDeadlineDeployment(t, c, clk)
	require.NoError(t, err)
	require.Zero(t, result.RequeueAfter)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, progressing(t, c).Reason)
	clk.Step(time.Hour)
	markCOSReady(t, c, cos.Name, false)
	result, err = reconcileDeadlineDeployment(t, c, clk)
	require.NoError(t, err)
	require.Zero(t, result.RequeueAfter)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed, progressing(t, c).Reason)
}

func TestClusterObjectDeploymentReconcilerDeadlineExcludesCreationRetries(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
	cod := codFixture()
	cod.Spec.Template.Spec.ProgressDeadlineMinutes = 10
	cod.CreationTimestamp = metav1.NewTime(clk.Now())
	creationErr := errors.New("revision creation denied")
	denied := true
	c := interceptor.NewClient(newDeadlineDeploymentClient(t, cod, clk), interceptor.Funcs{
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if denied {
				return creationErr
			}
			return cl.Create(ctx, obj, opts...)
		},
	})
	for range 2 {
		result, err := reconcileDeadlineDeployment(t, c, clk)
		require.ErrorIs(t, err, creationErr)
		require.Zero(t, result.RequeueAfter)
		require.Equal(t, metav1.ConditionTrue, progressing(t, c).Status)
		clk.Step(20 * time.Minute)
	}
	denied = false
	result, err := reconcileDeadlineDeployment(t, c, clk)
	require.NoError(t, err)
	require.Equal(t, 10*time.Minute, result.RequeueAfter)
	require.Equal(t, metav1.ConditionTrue, progressing(t, c).Status)
}

func TestClusterObjectDeploymentReconcilerDeadlineRetainsAvailability(t *testing.T) {
	clk := clocktesting.NewFakeClock(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
	cod := codFixture()
	cod.Spec.Template.Spec.ProgressDeadlineMinutes = 10
	c := newDeadlineDeploymentClient(t, cod, clk)
	_, err := reconcileDeadlineDeployment(t, c, clk)
	require.NoError(t, err)
	markCOSReady(t, c, cod.Name+"-1", true)
	cos, found := getCOS(t, c, cod.Name+"-1")
	require.True(t, found)
	cos.Status.CompletedAt = metav1.NewTime(clk.Now())
	require.NoError(t, c.Status().Update(t.Context(), cos))
	_, err = reconcileDeadlineDeployment(t, c, clk)
	require.NoError(t, err)

	clk.Step(time.Hour)
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(cod), cod))
	cod.Spec.Template.Spec.Phases[0].Objects[0].Object.Object["data"] = map[string]interface{}{"version": "second"}
	require.NoError(t, c.Update(t.Context(), cod))
	result, err := reconcileDeadlineDeployment(t, c, clk)
	require.NoError(t, err)
	require.Equal(t, 10*time.Minute, result.RequeueAfter)
	cos, found = getCOS(t, c, cod.Name+"-2")
	require.True(t, found)
	cos.Status.Conditions = []metav1.Condition{{
		Type: ocv1.ClusterObjectSetTypeReady, Status: metav1.ConditionFalse, Reason: ocv1.ClusterObjectSetReasonPreconditionsNotMet,
	}}
	require.NoError(t, c.Status().Update(t.Context(), cos))
	clk.Step(10 * time.Minute)
	result, err = reconcileDeadlineDeployment(t, c, clk)
	require.NoError(t, err)
	require.Zero(t, result.RequeueAfter)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded, progressing(t, c).Reason)
	require.Equal(t, metav1.ConditionTrue, available(t, c).Status)
}
