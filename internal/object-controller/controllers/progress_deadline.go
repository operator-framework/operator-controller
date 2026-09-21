//go:build !standard

package controllers

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// deadlineAwareRateLimiter wraps a delegate rate limiter and caps the backoff
// duration to the time remaining until the COS progress deadline, ensuring
// that ProgressDeadlineExceeded is set promptly even during exponential backoff.
//
// After the deadline passes, it allows one immediate requeue (returning 0) so
// the reconciler can set the ProgressDeadlineExceeded condition, then falls
// back to the delegate's normal backoff. This avoids both tight-looping and
// coupling to the COS's status conditions.
type deadlineAwareRateLimiter struct {
	delegate     workqueue.TypedRateLimiter[ctrl.Request]
	client       client.Reader
	clock        clock.Clock
	pastDeadline sync.Map
}

func newDeadlineAwareRateLimiter(
	delegate workqueue.TypedRateLimiter[ctrl.Request],
	c client.Reader,
	clk clock.Clock,
) *deadlineAwareRateLimiter {
	return &deadlineAwareRateLimiter{delegate: delegate, client: c, clock: clk}
}

func (r *deadlineAwareRateLimiter) When(item ctrl.Request) time.Duration {
	backoff := r.delegate.When(item)

	cos := &ocv1.ClusterObjectSet{}
	if err := r.client.Get(context.Background(), item.NamespacedName, cos); err != nil {
		return backoff
	}

	remaining, hasDeadline := durationUntilDeadline(r.clock, cos)
	if !hasDeadline {
		return backoff
	}
	if remaining > 0 {
		if remaining < backoff {
			return remaining
		}
		return backoff
	}

	// Deadline has passed — allow one immediate requeue, then delegate.
	if _, already := r.pastDeadline.LoadOrStore(item, struct{}{}); !already {
		return 0
	}
	return backoff
}

func (r *deadlineAwareRateLimiter) Forget(item ctrl.Request) {
	r.delegate.Forget(item)
	r.pastDeadline.Delete(item)
}

func (r *deadlineAwareRateLimiter) NumRequeues(item ctrl.Request) int {
	return r.delegate.NumRequeues(item)
}

// durationUntilDeadline returns how much time remains before the progress deadline
// expires. A negative duration means the deadline has already passed.
//
// It derives the deadline from spec and metadata only, with one exception:
// it checks the Succeeded status condition so that a revision recovering
// from drift is not penalised by the original deadline.
//
// Succeeded is a latch: there is no way to deduce from current cluster state
// alone that a COS succeeded in the past. If Succeeded is removed or set to
// False, this function will return a deadline and the reconciler will set
// ProgressDeadlineExceeded even though the revision previously succeeded.
//
// Returns (0, false) when there is no active deadline:
//   - progressDeadlineMinutes is 0
//   - the revision has already succeeded
//   - the revision is archived (deadline is irrelevant)
//   - the revision is being deleted
func durationUntilDeadline(clk clock.Clock, cos *ocv1.ClusterObjectSet) (time.Duration, bool) {
	pd := cos.Spec.ProgressDeadlineMinutes
	if pd <= 0 {
		return 0, false
	}
	if meta.IsStatusConditionTrue(cos.Status.Conditions, ocv1.ClusterObjectSetTypeSucceeded) {
		return 0, false
	}
	if cos.Spec.LifecycleState == ocv1.ClusterObjectSetLifecycleStateArchived {
		return 0, false
	}
	if !cos.DeletionTimestamp.IsZero() {
		return 0, false
	}
	deadline := cos.CreationTimestamp.Add(time.Duration(pd) * time.Minute)
	return deadline.Sub(clk.Now()), true
}
