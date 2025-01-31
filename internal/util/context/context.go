package context

import (
	"context"
	"time"
)

func (d *delayContext) Deadline() (time.Time, bool) {
	select {
	case <-d.parentCtx.Done():
		// if the parent context is done, wait
		// for our timeout setup to complete, then
		// return the timeout context's deadline.
		<-d.setupDone
		return d.timeoutCtx.Deadline()
	default:
		// if the parent context has a deadline, simply add
		// our delay.
		if parentDeadline, ok := d.parentCtx.Deadline(); ok {
			return parentDeadline.Add(d.delay), true
		}
		// if the parent context does not have a deadline
		// then we don't know ours either because it depends
		// on when the parent is done.
		return time.Time{}, false
	}
}

func (d *delayContext) Done() <-chan struct{} {
	return d.done
}

func (d *delayContext) Err() error {
	// If the parent context is done, wait until setup
	// is done, then return the timeout context's error.
	select {
	case <-d.parentCtx.Done():
		<-d.setupDone
		return d.timeoutCtx.Err()
	default:
	}

	// If done is closed, that means we were
	// directly cancelled. Otherwise (if neither
	// parent context is done or done is closed)
	// the context is still active, hence no error
	select {
	case <-d.done:
		return context.Canceled
	default:
		return nil
	}
}

func (d *delayContext) Value(key interface{}) interface{} {
	return d.parentCtx.Value(key)
}

type delayContext struct {
	parentCtx context.Context
	delay     time.Duration

	done      chan struct{}
	setupDone chan struct{}

	timeoutCtx    context.Context
	timeoutCancel context.CancelFunc
}

func WithDelay(parentCtx context.Context, delay time.Duration) (context.Context, context.CancelFunc) {
	delayedCtx := &delayContext{
		parentCtx: parentCtx,
		delay:     delay,
		done:      make(chan struct{}),
		setupDone: make(chan struct{}),
	}

	setupDelay := func() {
		timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), delay)
		context.AfterFunc(timeoutCtx, func() { close(delayedCtx.done) })
		delayedCtx.timeoutCtx = timeoutCtx
		delayedCtx.timeoutCancel = timeoutCancel
		close(delayedCtx.setupDone)
	}

	unregisterDelay := context.AfterFunc(parentCtx, setupDelay)

	cancelFunc := func() {
		setupNeverHappened := unregisterDelay()
		if setupNeverHappened {
			// if setup never happened, then the delay context was
			// cancelled prior to the parent context being done.
			//
			// all we need to do here is close the done chan.
			close(delayedCtx.done)
		} else {
			// if we're here, the setup function was called

			// wait until setup is done to ensure there is a
			// timeoutContext/timeoutCancel
			<-delayedCtx.setupDone

			// cancel the timeout context (which includes
			// an AfterFunc to also close our doneChan, so
			// we'll wait for that to be closed before
			// returning)
			delayedCtx.timeoutCancel()
			<-delayedCtx.done
		}
	}

	return delayedCtx, cancelFunc
}
