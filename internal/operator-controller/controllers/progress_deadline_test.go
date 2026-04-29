//go:build !standard

package controllers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clocktesting "k8s.io/utils/clock/testing"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
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
			name: "Succeeded is true — no deadline",
			cos: ocv1.ClusterObjectSet{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(creation)},
				Spec:       ocv1.ClusterObjectSetSpec{ProgressDeadlineMinutes: 1, LifecycleState: ocv1.ClusterObjectSetLifecycleStateActive},
				Status: ocv1.ClusterObjectSetStatus{
					Conditions: []metav1.Condition{{
						Type:   ocv1.ClusterObjectSetTypeSucceeded,
						Status: metav1.ConditionTrue,
					}},
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
