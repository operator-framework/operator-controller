package steps_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

type fakeCommandRecorder struct{}

func (*fakeCommandRecorder) RecordCommand(string, string, string, time.Duration) {}
func (*fakeCommandRecorder) RecordCommandWithInput(string, string, string, string, time.Duration) {
}
func (*fakeCommandRecorder) RecordCustom(string, string, string) {}

func TestCommandRecorderFromContextWithoutRecorder(t *testing.T) {
	require.Nil(t, steps.CommandRecorderFromContext(context.Background()))
}

func TestWithCommandRecorder(t *testing.T) {
	parent := context.Background()
	recorder := &fakeCommandRecorder{}

	ctx := steps.WithCommandRecorder(parent, recorder)

	require.Same(t, recorder, steps.CommandRecorderFromContext(ctx))
	require.Nil(t, steps.CommandRecorderFromContext(parent))
}
