package steps_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

func TestWithRecorder(t *testing.T) {
	parent := steps.WithCommandRecorder(context.Background(), &steps.AsciiCastRecorder{})
	recorder := &steps.AsciiCastRecorder{}

	ctx := steps.WithRecorder(parent, recorder)

	require.Same(t, recorder, steps.RecorderFromContext(ctx))
	require.Same(t, recorder, steps.CommandRecorderFromContext(ctx))
	require.NotSame(t, recorder, steps.CommandRecorderFromContext(parent))
	require.Nil(t, steps.RecorderFromContext(parent))
}
