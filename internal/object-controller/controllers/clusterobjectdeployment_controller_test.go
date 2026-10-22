//go:build !standard

package controllers_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/object-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
	"github.com/operator-framework/operator-controller/internal/testutil"
)

const clusterObjectDeploymentName = "test-cod-1"

func codFixture() *ocv1.ClusterObjectDeployment {
	return &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: clusterObjectDeploymentName},
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
								"metadata":   map[string]interface{}{"name": "cm", "namespace": "ns"},
							}},
						}},
					}},
				},
			},
		},
	}
}

func newCODClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	return testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(newScheme(t)), controllers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectDeployment{}, &ocv1.ClusterObjectSet{}).
		WithObjects(objs...).
		Build()
}

func reconcileCOD(t *testing.T, c client.Client) (ctrl.Result, error) {
	t.Helper()
	return (&controllers.ClusterObjectDeploymentReconciler{Client: c, Scheme: newScheme(t)}).
		Reconcile(t.Context(), ctrl.Request{NamespacedName: types.NamespacedName{Name: clusterObjectDeploymentName}})
}

func getCOS(t *testing.T, c client.Client, name string) (*ocv1.ClusterObjectSet, bool) {
	t.Helper()
	cos := &ocv1.ClusterObjectSet{}
	err := c.Get(t.Context(), types.NamespacedName{Name: name}, cos)
	if apierrors.IsNotFound(err) {
		return nil, false
	}
	require.NoError(t, err)
	return cos, true
}

func progressing(t *testing.T, c client.Client) *metav1.Condition {
	t.Helper()
	cod := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: clusterObjectDeploymentName}, cod))
	return apimeta.FindStatusCondition(cod.Status.Conditions, ocv1.ClusterObjectDeploymentTypeProgressing)
}

func available(t *testing.T, c client.Client) *metav1.Condition {
	t.Helper()
	cod := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: clusterObjectDeploymentName}, cod))
	return apimeta.FindStatusCondition(cod.Status.Conditions, ocv1.ClusterObjectDeploymentTypeAvailable)
}

func markCOSReady(t *testing.T, c client.Client, name string, ready bool) {
	t.Helper()
	cos, ok := getCOS(t, c, name)
	require.True(t, ok)
	status, reason := metav1.ConditionFalse, ocv1.ClusterObjectSetReasonObjectsNotReady
	if ready {
		status, reason = metav1.ConditionTrue, ocv1.ClusterObjectSetReasonAllObjectsReady
	}
	apimeta.SetStatusCondition(&cos.Status.Conditions, metav1.Condition{
		Type:    ocv1.ClusterObjectSetTypeReady,
		Status:  status,
		Reason:  reason,
		Message: "test",
	})
	require.NoError(t, c.Status().Update(t.Context(), cos))
}

// markCOSProgressDeadlineExceeded sets the named revision's Ready condition to
// False with reason ProgressDeadlineExceeded, mirroring what the ClusterObjectSet
// controller records when a revision does not roll out within its deadline.
func markCOSProgressDeadlineExceeded(t *testing.T, c client.Client, name string) {
	t.Helper()
	cos, ok := getCOS(t, c, name)
	require.True(t, ok)
	apimeta.SetStatusCondition(&cos.Status.Conditions, metav1.Condition{
		Type:    ocv1.ClusterObjectSetTypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  ocv1.ReasonProgressDeadlineExceeded,
		Message: "test",
	})
	require.NoError(t, c.Status().Update(t.Context(), cos))
}

func Test_ClusterObjectDeploymentReconciler_Available(t *testing.T) {
	t.Run("no active revision before the first revision exists", func(t *testing.T) {
		c := newCODClient(t, codFixture())
		_, err := reconcileCOD(t, c)
		require.NoError(t, err)

		cond := available(t, c)
		require.NotNil(t, cond)
		require.Equal(t, metav1.ConditionFalse, cond.Status)
		require.Equal(t, ocv1.ClusterObjectDeploymentReasonNoActiveRevision, cond.Reason)
	})

	t.Run("unavailable when the active revision is not Ready", func(t *testing.T) {
		c := newCODClient(t, codFixture())
		_, err := reconcileCOD(t, c) // creates revision 1
		require.NoError(t, err)
		_, err = reconcileCOD(t, c) // revision 1 is now listed but not Ready
		require.NoError(t, err)

		cond := available(t, c)
		require.NotNil(t, cond)
		require.Equal(t, metav1.ConditionFalse, cond.Status)
		require.Equal(t, ocv1.ClusterObjectDeploymentReasonUnavailable, cond.Reason)
	})

	t.Run("available when an active revision is Ready", func(t *testing.T) {
		c := newCODClient(t, codFixture())
		_, err := reconcileCOD(t, c) // creates revision 1
		require.NoError(t, err)
		markCOSReady(t, c, clusterObjectDeploymentName+"-1", true)
		_, err = reconcileCOD(t, c)
		require.NoError(t, err)

		cond := available(t, c)
		require.NotNil(t, cond)
		require.Equal(t, metav1.ConditionTrue, cond.Status)
		require.Equal(t, ocv1.ClusterObjectDeploymentReasonAvailable, cond.Reason)
	})

	t.Run("stays available while a new revision rolls out", func(t *testing.T) {
		c := newCODClient(t, codFixture())
		_, err := reconcileCOD(t, c) // creates revision 1
		require.NoError(t, err)
		markCOSReady(t, c, clusterObjectDeploymentName+"-1", true)
		_, err = reconcileCOD(t, c)
		require.NoError(t, err)
		require.Equal(t, metav1.ConditionTrue, available(t, c).Status)

		// Change the template so a new revision is created while revision 1 stays Ready.
		cod := &ocv1.ClusterObjectDeployment{}
		require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: clusterObjectDeploymentName}, cod))
		cod.Spec.Template.Spec.Phases[0].Objects[0].Object.Object["data"] = map[string]interface{}{"k": "v"}
		require.NoError(t, c.Update(t.Context(), cod))

		_, err = reconcileCOD(t, c) // creates revision 2; revision 1 is still active and Ready
		require.NoError(t, err)

		cond := available(t, c)
		require.NotNil(t, cond)
		require.Equal(t, metav1.ConditionTrue, cond.Status, "deployment must stay available while a prior revision is still Ready")
	})

	t.Run("stays available when a new revision exceeds its progress deadline but a prior revision is Ready", func(t *testing.T) {
		c := newCODClient(t, codFixture())
		_, err := reconcileCOD(t, c) // creates revision 1
		require.NoError(t, err)
		markCOSReady(t, c, clusterObjectDeploymentName+"-1", true)
		_, err = reconcileCOD(t, c)
		require.NoError(t, err)
		require.Equal(t, metav1.ConditionTrue, available(t, c).Status)

		// Change the template so a new revision is created while revision 1 stays Ready.
		cod := &ocv1.ClusterObjectDeployment{}
		require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: clusterObjectDeploymentName}, cod))
		cod.Spec.Template.Spec.Phases[0].Objects[0].Object.Object["data"] = map[string]interface{}{"k": "v"}
		require.NoError(t, c.Update(t.Context(), cod))

		_, err = reconcileCOD(t, c) // creates revision 2
		require.NoError(t, err)

		// Revision 2 blows its deadline while revision 1 is still Active and Ready.
		markCOSProgressDeadlineExceeded(t, c, clusterObjectDeploymentName+"-2")
		_, err = reconcileCOD(t, c)
		require.NoError(t, err)

		require.Equal(t, metav1.ConditionTrue, available(t, c).Status,
			"deployment must stay available while a prior revision is still Ready, even if the new revision exceeds its deadline")

		prog := progressing(t, c)
		require.NotNil(t, prog)
		require.Equal(t, metav1.ConditionFalse, prog.Status)
		require.Equal(t, ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded, prog.Reason,
			"Progressing must surface the deadline breach of the latest revision")
	})
}

func Test_ClusterObjectDeploymentReconciler_CreatesFirstRevision(t *testing.T) {
	c := newCODClient(t, codFixture())

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)

	cos, ok := getCOS(t, c, clusterObjectDeploymentName+"-1")
	require.True(t, ok, "revision 1 should be created")
	require.Equal(t, clusterObjectDeploymentName, cos.Spec.Group)
	require.Equal(t, int64(1), cos.Spec.Revision)
	require.Equal(t, ocv1.ClusterObjectSetLifecycleStateActive, cos.Spec.LifecycleState)
	require.Equal(t, ocv1.CollisionProtectionPrevent, cos.Spec.CollisionProtection)
	require.Equal(t, int32(15), cos.Spec.ProgressDeadlineMinutes, "progressDeadlineMinutes should pass through to the revision")
	require.NotEmpty(t, cos.Labels[labels.TemplateHashKey])

	ref := metav1.GetControllerOf(cos)
	require.NotNil(t, ref)
	require.Equal(t, ocv1.ClusterObjectDeploymentKind, ref.Kind)
	require.Equal(t, clusterObjectDeploymentName, ref.Name)

	cond := progressing(t, c)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing, cond.Reason)
}

func Test_ClusterObjectDeploymentReconciler_NoNewRevisionWhenTemplateUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name       string
		prep       func(t *testing.T, c client.Client, cosName string)
		wantStatus metav1.ConditionStatus
		wantReason string
	}{
		{
			name: "completed latest revision reports Progressed",
			prep: func(t *testing.T, c client.Client, cosName string) {
				cos, ok := getCOS(t, c, cosName)
				require.True(t, ok)
				cos.Status.CompletedAt = metav1.Now()
				require.NoError(t, c.Status().Update(t.Context(), cos))
			},
			wantStatus: metav1.ConditionFalse,
			wantReason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressed,
		},
		{
			name:       "not-yet-completed latest revision reports Progressing",
			prep:       nil,
			wantStatus: metav1.ConditionTrue,
			wantReason: ocv1.ClusterObjectDeploymentReasonNewClusterObjectSetProgressing,
		},
		{
			name:       "deadline-exceeded latest revision reports ProgressDeadlineExceeded",
			prep:       func(t *testing.T, c client.Client, cosName string) { markCOSProgressDeadlineExceeded(t, c, cosName) },
			wantStatus: metav1.ConditionFalse,
			wantReason: ocv1.ClusterObjectDeploymentReasonProgressDeadlineExceeded,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCODClient(t, codFixture())

			// First reconcile creates revision 1 with the correct template-hash label.
			_, err := reconcileCOD(t, c)
			require.NoError(t, err)

			if tc.prep != nil {
				tc.prep(t, c, clusterObjectDeploymentName+"-1")
			}

			// Second reconcile must not create a new revision.
			_, err = reconcileCOD(t, c)
			require.NoError(t, err)

			_, ok := getCOS(t, c, clusterObjectDeploymentName+"-2")
			require.False(t, ok, "no second revision should be created when the template is unchanged")

			cond := progressing(t, c)
			require.NotNil(t, cond)
			require.Equal(t, tc.wantStatus, cond.Status)
			require.Equal(t, tc.wantReason, cond.Reason)
		})
	}
}

func Test_ClusterObjectDeploymentReconciler_CreatesNewRevisionOnTemplateChange(t *testing.T) {
	c := newCODClient(t, codFixture())

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)

	// Change the template.
	cod := &ocv1.ClusterObjectDeployment{}
	require.NoError(t, c.Get(t.Context(), types.NamespacedName{Name: clusterObjectDeploymentName}, cod))
	cod.Spec.Template.Spec.Phases[0].Objects[0].Object.Object["data"] = map[string]interface{}{"k": "v"}
	require.NoError(t, c.Update(t.Context(), cod))

	_, err = reconcileCOD(t, c)
	require.NoError(t, err)

	cos, ok := getCOS(t, c, clusterObjectDeploymentName+"-2")
	require.True(t, ok, "a template change should create revision 2")
	require.Equal(t, int64(2), cos.Spec.Revision)

	cond := progressing(t, c)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
}

func Test_ClusterObjectDeploymentReconciler_CreatesNewRevisionWhenLatestArchived(t *testing.T) {
	c := newCODClient(t, codFixture())

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)

	// Archive revision 1.
	cos, ok := getCOS(t, c, clusterObjectDeploymentName+"-1")
	require.True(t, ok)
	cos.Spec.LifecycleState = ocv1.ClusterObjectSetLifecycleStateArchived
	require.NoError(t, c.Update(t.Context(), cos))

	_, err = reconcileCOD(t, c)
	require.NoError(t, err)

	rev2, ok := getCOS(t, c, clusterObjectDeploymentName+"-2")
	require.True(t, ok, "archiving the only revision should trigger a new revision")
	require.Equal(t, int64(2), rev2.Spec.Revision)
}

func Test_ClusterObjectDeploymentReconciler_DeletionIsNoop(t *testing.T) {
	cod := codFixture()
	now := metav1.Now()
	cod.DeletionTimestamp = &now
	cod.Finalizers = []string{"test.only/keep"}

	c := newCODClient(t, cod)

	_, err := reconcileCOD(t, c)
	require.NoError(t, err)

	_, ok := getCOS(t, c, clusterObjectDeploymentName+"-1")
	require.False(t, ok, "a deleting ClusterObjectDeployment must not create revisions")

	require.Nil(t, progressing(t, c))
}

func Test_ClusterObjectDeploymentReconciler_SurfacesAlreadyExists(t *testing.T) {
	base := testutil.WithIndexes(t, fake.NewClientBuilder().WithScheme(newScheme(t)), controllers.SetupIndexes).
		WithStatusSubresource(&ocv1.ClusterObjectDeployment{}, &ocv1.ClusterObjectSet{}).
		WithObjects(codFixture()).
		Build()

	c := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.CreateOption) error {
			gr := ocv1.GroupVersion.WithResource("clusterobjectsets").GroupResource()
			return apierrors.NewAlreadyExists(gr, obj.GetName())
		},
	})

	_, err := (&controllers.ClusterObjectDeploymentReconciler{Client: c, Scheme: newScheme(t)}).
		Reconcile(t.Context(), ctrl.Request{NamespacedName: types.NamespacedName{Name: clusterObjectDeploymentName}})
	require.Error(t, err, "an AlreadyExists on create must surface as a reconcile error, not be silently tolerated")
}
