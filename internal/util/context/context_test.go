package context_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	contextutil "github.com/operator-framework/operator-controller/internal/util/context"
)

func TestWithDelay_Delays(t *testing.T) {
	for _, delay := range []time.Duration{
		0,
		time.Millisecond * 10,
		time.Millisecond * 100,
		time.Millisecond * 200,
	} {
		t.Run(delay.String(), func(t *testing.T) {
			parentCtx, parentCancel := context.WithCancel(context.Background())
			delayCtx, _ := contextutil.WithDelay(parentCtx, delay)

			parentCancel()

			// verify deadline is within 1m ms of what we expect
			expectDeadline := time.Now().Add(delay)
			actualDeadline, ok := delayCtx.Deadline()
			assert.True(t, ok, "expected delay context to have a deadline after parent was cancelled")
			assert.WithinDurationf(t, expectDeadline, actualDeadline, time.Millisecond, "expected the context's deadline (%v) to be within 1 ms of %v; diff was %v", expectDeadline, actualDeadline, expectDeadline.Sub(actualDeadline))

			// verify context is done due to deadline exceeded and that it happens
			// within 3ms of our expectation
			select {
			case <-delayCtx.Done():
			case <-time.After(time.Until(expectDeadline.Add(3 * time.Millisecond))):
				diff := time.Since(expectDeadline)
				t.Fatalf("delay context should have been canceled quickly after %s, but it took %s", delay, diff)
			}
			assert.ErrorIs(t, delayCtx.Err(), context.DeadlineExceeded)
		})
	}
}

func TestWithDelay_Deadline(t *testing.T) {
	t.Run("parent has deadline", func(t *testing.T) {
		parentDeadline := time.Now().Add(200 * time.Millisecond)
		parentCtx, parentCancel := context.WithDeadline(context.Background(), parentDeadline)
		defer parentCancel()

		delay := 250 * time.Millisecond
		delayCtx, _ := contextutil.WithDelay(parentCtx, delay)

		expectDeadline := parentDeadline.Add(delay)
		actualDeadline, ok := delayCtx.Deadline()

		assert.True(t, ok, "expected delay context to have a deadline before parent was cancelled")
		assert.Equal(t, expectDeadline, actualDeadline)
	})
	t.Run("parent has no deadline", func(t *testing.T) {
		parentCtx, parentCancel := context.WithCancel(context.Background())
		defer parentCancel()

		delayCtx, _ := contextutil.WithDelay(parentCtx, 200*time.Millisecond)
		actualDeadline, ok := delayCtx.Deadline()
		assert.False(t, ok, "expected delay context to have an unknown deadline before parent was cancelled")
		assert.Equal(t, time.Time{}, actualDeadline, "expected delay context deadline to be unset")
	})
}

func TestWithDelay_Err(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		delayCtx, _ := contextutil.WithDelay(context.Background(), 0)
		assert.NoError(t, delayCtx.Err())
	})
	t.Run("canceled before parent done", func(t *testing.T) {
		delayCtx, delayCancel := contextutil.WithDelay(context.Background(), 0)
		delayCancel()
		assert.ErrorIs(t, delayCtx.Err(), context.Canceled)
	})
	t.Run("canceled after parent done", func(t *testing.T) {
		parentCtx, parentCancel := context.WithCancel(context.Background())
		delayCtx, delayCancel := contextutil.WithDelay(parentCtx, 200*time.Millisecond)
		parentCancel()
		delayCancel()
		assert.ErrorIs(t, delayCtx.Err(), context.Canceled)
	})
	t.Run("deadline exceeded", func(t *testing.T) {
		parentCtx, parentCancel := context.WithCancel(context.Background())
		delayCtx, _ := contextutil.WithDelay(parentCtx, 0)
		parentCancel()
		assert.ErrorIs(t, delayCtx.Err(), context.DeadlineExceeded)
	})
}

func TestWithDelay_Value(t *testing.T) {
	type valueKey string
	parentCtx := context.WithValue(context.Background(), valueKey("foo"), "bar")
	delayCtx, _ := contextutil.WithDelay(parentCtx, 0)
	assert.Equal(t, "bar", delayCtx.Value(valueKey("foo")))
}
