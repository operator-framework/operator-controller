//go:build !standard

package controllers

import (
	"sync"
	"time"

	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
)

// deadlineAwareRateLimiter wraps a delegate rate limiter and caps the backoff
// duration to the time remaining until the deadline reported by its remaining
// function, ensuring that a progress deadline is observed promptly even during
// exponential backoff. The deadline source is supplied by the constructor (the
// ClusterObjectDeployment controller's progress deadline).
//
// After the deadline passes, it allows one immediate requeue (returning 0) so the
// reconciler can act on the expiry, then falls back to the delegate's normal
// backoff. This avoids both tight-looping and coupling to status conditions.
type deadlineAwareRateLimiter struct {
	delegate     workqueue.TypedRateLimiter[ctrl.Request]
	remaining    func(ctrl.Request) (time.Duration, bool, error)
	pastDeadline sync.Map
}

func (r *deadlineAwareRateLimiter) When(item ctrl.Request) time.Duration {
	backoff := r.delegate.When(item)

	remaining, hasDeadline, err := r.remaining(item)
	if err != nil {
		// An unknown deadline must not re-arm the immediate requeue after expiry.
		return backoff
	}
	if !hasDeadline {
		r.pastDeadline.Delete(item)
		return backoff
	}
	if remaining > 0 {
		r.pastDeadline.Delete(item)
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
