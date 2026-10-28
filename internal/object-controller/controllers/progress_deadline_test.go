//go:build !standard

package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clocktesting "k8s.io/utils/clock/testing"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/shared/labels"
)

func TestDurationUntilDeadline(t *testing.T) {
	creation := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	now := creation.Add(30 * time.Second)
	clk := clocktesting.NewFakeClock(now)

	for _, tc := range []struct {
		name              string
		cos               ocv1.ClusterObjectSet
		expectDuration    time.Duration
		expectHasDeadline bool
	}{
		{
			name: "progressDeadlineMinutes is 0 — no deadline",
			cos: ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(creation)},
				Spec:       ocv1.ClusterObjectSetSpec{ProgressDeadlineMinutes: 0, LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive},
			},
			expectDuration:    0,
			expectHasDeadline: false,
		},
		{
			name: "completedAt is set — no deadline",
			cos: ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(creation)},
				Spec:       ocv1.ClusterObjectSetSpec{ProgressDeadlineMinutes: 1, LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive},
				Status: ocv1.ClusterObjectSetStatus{
					CompletedAt: metav1.NewTime(creation),
				},
			},
			expectDuration:    0,
			expectHasDeadline: false,
		},
		{
			name: "lifecycleState is Archived — no deadline",
			cos: ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(creation)},
				Spec:       ocv1.ClusterObjectSetSpec{ProgressDeadlineMinutes: 1, LifecycleState: ocv1.ClusterObjectSetLifecycleStateArchived},
			},
			expectDuration:    0,
			expectHasDeadline: false,
		},
		{
			name: "DeletionTimestamp is set — no deadline",
			cos: ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(creation),
					DeletionTimestamp: &metav1.Time{Time: now},
				},
				Spec: ocv1.ClusterObjectSetSpec{ProgressDeadlineMinutes: 1, LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive},
			},
			expectDuration:    0,
			expectHasDeadline: false,
		},
		{
			name: "deadline not yet exceeded — returns positive remaining",
			cos: ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(creation)},
				Spec:       ocv1.ClusterObjectSetSpec{ProgressDeadlineMinutes: 1, LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive},
			},
			expectDuration:    30 * time.Second,
			expectHasDeadline: true,
		},
		{
			name: "deadline already exceeded — returns negative remaining",
			cos: ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(creation.Add(-2 * time.Minute))},
				Spec:       ocv1.ClusterObjectSetSpec{ProgressDeadlineMinutes: 1, LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive},
			},
			expectDuration:    -90 * time.Second,
			expectHasDeadline: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			duration, hasDeadline := durationUntilDeadline(clk, &tc.cos)
			require.Equal(t, tc.expectHasDeadline, hasDeadline)
			require.Equal(t, tc.expectDuration, duration)
		})
	}
}

type fixedRateLimiter struct {
	duration time.Duration
}

func (f *fixedRateLimiter) When(_ ctrl.Request) time.Duration { return f.duration }
func (f *fixedRateLimiter) Forget(_ ctrl.Request)             {}
func (f *fixedRateLimiter) NumRequeues(_ ctrl.Request) int    { return 0 }

func TestDeadlineAwareRateLimiter(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))

	creation := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-cos"}}

	for _, tc := range []struct {
		name           string
		backoff        time.Duration
		cos            *ocv1.ClusterObjectSet
		clockTime      time.Time
		expectDuration time.Duration
	}{
		{
			name:      "no deadline configured — uses delegate backoff",
			backoff:   30 * time.Second,
			clockTime: creation,
			cos: &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cos",
					CreationTimestamp: metav1.NewTime(creation),
				},
				Spec: ocv1.ClusterObjectSetSpec{
					LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive,
				},
			},
			expectDuration: 30 * time.Second,
		},
		{
			name:      "deadline not exceeded and backoff is shorter — uses delegate backoff",
			backoff:   5 * time.Second,
			clockTime: creation,
			cos: &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cos",
					CreationTimestamp: metav1.NewTime(creation),
				},
				Spec: ocv1.ClusterObjectSetSpec{
					LifecycleState:          ocv1.ClusterObjectSetLifecycleStateActive,
					ProgressDeadlineMinutes: 1,
				},
			},
			expectDuration: 5 * time.Second,
		},
		{
			name:      "deadline not exceeded and backoff is longer — caps at deadline",
			backoff:   5 * time.Minute,
			clockTime: creation,
			cos: &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cos",
					CreationTimestamp: metav1.NewTime(creation),
				},
				Spec: ocv1.ClusterObjectSetSpec{
					LifecycleState:          ocv1.ClusterObjectSetLifecycleStateActive,
					ProgressDeadlineMinutes: 1,
				},
			},
			expectDuration: 60 * time.Second,
		},
		{
			name:      "deadline exceeded — first call returns immediate requeue",
			backoff:   30 * time.Second,
			clockTime: creation.Add(61 * time.Second),
			cos: &ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-cos",
					CreationTimestamp: metav1.NewTime(creation),
				},
				Spec: ocv1.ClusterObjectSetSpec{
					LifecycleState:          ocv1.ClusterObjectSetLifecycleStateActive,
					ProgressDeadlineMinutes: 1,
				},
			},
			expectDuration: 0,
		},
		{
			name:           "COS not found — uses delegate backoff",
			backoff:        30 * time.Second,
			clockTime:      creation,
			cos:            nil,
			expectDuration: 30 * time.Second,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if tc.cos != nil {
				builder = builder.WithObjects(tc.cos)
			}

			limiter := newDeadlineAwareRateLimiter(
				&fixedRateLimiter{duration: tc.backoff},
				builder.Build(),
				clocktesting.NewFakeClock(tc.clockTime),
			)

			testReq := req
			if tc.cos == nil {
				testReq.Name = "nonexistent"
			}

			result := limiter.When(testReq)
			require.Equal(t, tc.expectDuration, result)
		})
	}

	t.Run("deadline exceeded — second call uses delegate backoff (one-shot)", func(t *testing.T) {
		cos := &ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-cos",
				CreationTimestamp: metav1.NewTime(creation),
			},
			Spec: ocv1.ClusterObjectSetSpec{
				LifecycleState:          ocv1.ClusterObjectSetLifecycleStateActive,
				ProgressDeadlineMinutes: 1,
			},
		}
		limiter := newDeadlineAwareRateLimiter(
			&fixedRateLimiter{duration: 30 * time.Second},
			fake.NewClientBuilder().WithScheme(scheme).WithObjects(cos).Build(),
			clocktesting.NewFakeClock(creation.Add(61*time.Second)),
		)

		first := limiter.When(req)
		require.Equal(t, time.Duration(0), first)

		second := limiter.When(req)
		require.Equal(t, 30*time.Second, second)
	})

	t.Run("Forget resets the one-shot flag", func(t *testing.T) {
		cos := &ocv1.ClusterObjectSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-cos",
				CreationTimestamp: metav1.NewTime(creation),
			},
			Spec: ocv1.ClusterObjectSetSpec{
				LifecycleState:          ocv1.ClusterObjectSetLifecycleStateActive,
				ProgressDeadlineMinutes: 1,
			},
		}
		limiter := newDeadlineAwareRateLimiter(
			&fixedRateLimiter{duration: 30 * time.Second},
			fake.NewClientBuilder().WithScheme(scheme).WithObjects(cos).Build(),
			clocktesting.NewFakeClock(creation.Add(61*time.Second)),
		)

		require.Equal(t, time.Duration(0), limiter.When(req))
		require.Equal(t, 30*time.Second, limiter.When(req))

		limiter.Forget(req)

		require.Equal(t, time.Duration(0), limiter.When(req))
	})
}

func TestCODDeadlineAwareRateLimiter(t *testing.T) {
	creation := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name       string
		elapsed    time.Duration
		backoff    time.Duration
		mutate     func(*ocv1.ClusterObjectDeployment, *ocv1.ClusterObjectSet)
		omitCOS    bool
		expectWait time.Duration
	}{
		{name: "cap backoff at revision deadline", elapsed: 9 * time.Minute, backoff: 15 * time.Minute, expectWait: time.Minute},
		{name: "keep shorter backoff", elapsed: time.Minute, backoff: 30 * time.Second, expectWait: 30 * time.Second},
		{name: "expired revision wakes immediately", elapsed: 10 * time.Minute, backoff: 15 * time.Minute},
		{name: "creation pending has no deadline", elapsed: time.Hour, backoff: 15 * time.Minute, omitCOS: true, expectWait: 15 * time.Minute},
		{
			name: "foreign revision has no deployment deadline", elapsed: time.Hour, backoff: 15 * time.Minute, expectWait: 15 * time.Minute,
			mutate: func(_ *ocv1.ClusterObjectDeployment, cos *ocv1.ClusterObjectSet) {
				cos.OwnerReferences[0].Name = "other-deployment"
				cos.OwnerReferences[0].UID = "foreign-uid"
			},
		},
		{
			name: "unadopted revision has no deployment deadline", elapsed: time.Hour, backoff: 15 * time.Minute, expectWait: 15 * time.Minute,
			mutate: func(_ *ocv1.ClusterObjectDeployment, cos *ocv1.ClusterObjectSet) {
				cos.OwnerReferences = nil
			},
		},
		{
			name: "changed template waits for its own revision", elapsed: time.Hour, backoff: 15 * time.Minute, expectWait: 15 * time.Minute,
			mutate: func(cod *ocv1.ClusterObjectDeployment, _ *ocv1.ClusterObjectSet) {
				cod.Spec.Template.Spec.CollisionProtection = ocv1.CollisionProtectionIfNoController
			},
		},
		{
			name: "completed revision has no deadline", elapsed: time.Hour, backoff: 15 * time.Minute, expectWait: 15 * time.Minute,
			mutate: func(_ *ocv1.ClusterObjectDeployment, cos *ocv1.ClusterObjectSet) {
				cos.Status.CompletedAt = metav1.NewTime(creation.Add(time.Minute))
			},
		},
		{
			name: "disabled deadline", elapsed: time.Hour, backoff: 15 * time.Minute, expectWait: 15 * time.Minute,
			mutate: func(cod *ocv1.ClusterObjectDeployment, cos *ocv1.ClusterObjectSet) {
				cod.Spec.Template.Spec.ProgressDeadlineMinutes = 0
				cos.Labels[labels.TemplateHashKey] = templateHash(cod.Spec.Template)
			},
		},
		{
			name: "creation metadata not yet available", elapsed: time.Hour, backoff: 15 * time.Minute, expectWait: 15 * time.Minute,
			mutate: func(_ *ocv1.ClusterObjectDeployment, cos *ocv1.ClusterObjectSet) {
				cos.CreationTimestamp = metav1.Time{}
			},
		},
		{
			name: "archived revision is ignored", elapsed: time.Hour, backoff: 15 * time.Minute, expectWait: 15 * time.Minute,
			mutate: func(_ *ocv1.ClusterObjectDeployment, cos *ocv1.ClusterObjectSet) {
				cos.Spec.LifecycleState = ocv1.ClusterObjectSetLifecycleStateArchived
			},
		},
		{
			name: "deleting deployment is ignored", elapsed: time.Hour, backoff: 15 * time.Minute, expectWait: 15 * time.Minute,
			mutate: func(cod *ocv1.ClusterObjectDeployment, _ *ocv1.ClusterObjectSet) {
				cod.Finalizers = []string{"test"}
				cod.DeletionTimestamp = &metav1.Time{Time: creation}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cod, cos := deadlineRateLimiterObjects(creation)
			if tc.mutate != nil {
				tc.mutate(cod, cos)
			}
			objects := []client.Object{cod}
			if !tc.omitCOS {
				objects = append(objects, cos)
			}
			limiter := newCODDeadlineAwareRateLimiter(
				&fixedRateLimiter{duration: tc.backoff},
				deadlineRateLimiterClient(t, objects...),
				clocktesting.NewFakeClock(creation.Add(tc.elapsed)),
			)
			require.Equal(t, tc.expectWait, limiter.When(ctrl.Request{NamespacedName: types.NamespacedName{Name: cod.Name}}))
		})
	}

	t.Run("a new revision gets another deadline wakeup", func(t *testing.T) {
		cod, cos := deadlineRateLimiterObjects(creation)
		c := deadlineRateLimiterClient(t, cod, cos)
		clk := clocktesting.NewFakeClock(creation.Add(10 * time.Minute))
		limiter := newCODDeadlineAwareRateLimiter(&fixedRateLimiter{duration: 15 * time.Minute}, c, clk)
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: cod.Name}}
		require.Zero(t, limiter.When(req))
		require.Equal(t, 15*time.Minute, limiter.When(req), "an expired rollout must not busy-loop")

		next := cos.DeepCopy()
		next.Name = cod.Name + "-2"
		next.ResourceVersion = ""
		next.Spec.Revision = 2
		next.CreationTimestamp = metav1.NewTime(clk.Now())
		require.NoError(t, c.Create(t.Context(), next))
		require.Equal(t, 10*time.Minute, limiter.When(req))
		clk.Step(10 * time.Minute)
		require.Zero(t, limiter.When(req))
		require.Equal(t, 15*time.Minute, limiter.When(req))
	})
}

func TestCODDeadlineAwareRateLimiter_ReadFailures(t *testing.T) {
	for _, operation := range []string{"get", "list"} {
		t.Run(operation, func(t *testing.T) {
			creation := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
			cod, cos := deadlineRateLimiterObjects(creation)
			readErr := errors.New("temporary API read failure")
			failRead := false
			reader := interceptor.NewClient(deadlineRateLimiterClient(t, cod, cos), interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if failRead && operation == "get" {
						return readErr
					}
					return c.Get(ctx, key, obj, opts...)
				},
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if failRead && operation == "list" {
						return readErr
					}
					return c.List(ctx, list, opts...)
				},
			})
			limiter := newCODDeadlineAwareRateLimiter(
				&fixedRateLimiter{duration: 15 * time.Minute}, reader,
				clocktesting.NewFakeClock(creation.Add(10*time.Minute)),
			)
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: cod.Name}}
			require.Zero(t, limiter.When(req))
			for range 2 {
				failRead = true
				require.Equal(t, 15*time.Minute, limiter.When(req))
				failRead = false
				require.Equal(t, 15*time.Minute, limiter.When(req), "read recovery must not re-arm the immediate deadline requeue")
			}
		})
	}
}

func TestCODDeadlineAwareRateLimiter_DeadlineRemoved(t *testing.T) {
	creation := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name            string
		getErr          error
		mutateCOD       func(*ocv1.ClusterObjectDeployment)
		mutateRevisions func(*ocv1.ClusterObjectSetList)
	}{
		{
			name:   "deployment absent",
			getErr: apierrors.NewNotFound(ocv1.GroupVersion.WithResource("clusterobjectdeployments").GroupResource(), "test-cod"),
		},
		{
			name: "deployment deleting",
			mutateCOD: func(cod *ocv1.ClusterObjectDeployment) {
				cod.DeletionTimestamp = &metav1.Time{Time: creation}
			},
		},
		{
			name: "revision absent",
			mutateRevisions: func(revisions *ocv1.ClusterObjectSetList) {
				revisions.Items = nil
			},
		},
		{
			name: "revision deleting",
			mutateRevisions: func(revisions *ocv1.ClusterObjectSetList) {
				revisions.Items[0].DeletionTimestamp = &metav1.Time{Time: creation}
			},
		},
		{
			name: "revision ownership changes",
			mutateRevisions: func(revisions *ocv1.ClusterObjectSetList) {
				revisions.Items[0].OwnerReferences[0].UID = "old-deployment-uid"
			},
		},
		{
			name: "foreign leading revision appears",
			mutateRevisions: func(revisions *ocv1.ClusterObjectSetList) {
				foreign := revisions.Items[0].DeepCopy()
				foreign.Name += "-foreign"
				foreign.Spec.Revision++
				foreign.OwnerReferences[0].Name = "other-deployment"
				foreign.OwnerReferences[0].UID = "foreign-uid"
				revisions.Items = append(revisions.Items, *foreign)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cod, cos := deadlineRateLimiterObjects(creation)
			removeDeadline := false
			reader := interceptor.NewClient(deadlineRateLimiterClient(t, cod, cos), interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if removeDeadline && tc.getErr != nil {
						return tc.getErr
					}
					if err := c.Get(ctx, key, obj, opts...); err != nil {
						return err
					}
					if removeDeadline && tc.mutateCOD != nil {
						tc.mutateCOD(obj.(*ocv1.ClusterObjectDeployment))
					}
					return nil
				},
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if err := c.List(ctx, list, opts...); err != nil {
						return err
					}
					if removeDeadline && tc.mutateRevisions != nil {
						tc.mutateRevisions(list.(*ocv1.ClusterObjectSetList))
					}
					return nil
				},
			})
			limiter := newCODDeadlineAwareRateLimiter(
				&fixedRateLimiter{duration: 15 * time.Minute}, reader,
				clocktesting.NewFakeClock(creation.Add(10*time.Minute)),
			)
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: cod.Name}}
			require.Zero(t, limiter.When(req))
			require.Equal(t, 15*time.Minute, limiter.When(req))
			removeDeadline = true
			require.Equal(t, 15*time.Minute, limiter.When(req))
			removeDeadline = false
			require.Zero(t, limiter.When(req), "a definitively absent deadline must reset the immediate deadline requeue")
			require.Equal(t, 15*time.Minute, limiter.When(req))
		})
	}
}

func deadlineRateLimiterObjects(creation time.Time) (*ocv1.ClusterObjectDeployment, *ocv1.ClusterObjectSet) {
	cod := &ocv1.ClusterObjectDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cod", UID: "test-cod-uid"},
		Spec:       ocv1.ClusterObjectDeploymentSpec{Template: testTemplate()},
	}
	cod.Spec.Template.Spec.ProgressDeadlineMinutes = 10
	cos := buildClusterObjectSet(cod, 1, templateHash(cod.Spec.Template))
	cos.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(cod, ocv1.GroupVersion.WithKind(ocv1.ClusterObjectDeploymentKind))}
	cos.CreationTimestamp = metav1.NewTime(creation)
	return cod, cos
}

func deadlineRateLimiterClient(t *testing.T, objects ...client.Object) client.WithWatch {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, ocv1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).
		WithIndex(&ocv1.ClusterObjectSet{}, clusterObjectSetGroupIndex, func(obj client.Object) []string {
			return []string{obj.(*ocv1.ClusterObjectSet).Spec.Group}
		}).Build()
}
