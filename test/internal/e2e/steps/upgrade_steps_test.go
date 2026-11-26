package steps

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitForUpgradeCheckProvidesPollingDeadline(t *testing.T) {
	ctx := t.Context()
	wantEarliestDeadline := time.Now().Add(Timeout)
	var checkDeadline time.Time
	var hasDeadline bool

	err := WaitForUpgradeCheck(ctx, func(ctx context.Context) error {
		checkDeadline, hasDeadline = ctx.Deadline()
		return nil
	})

	require.NoError(t, err)
	require.True(t, hasDeadline)
	require.WithinRange(t, checkDeadline, wantEarliestDeadline, time.Now().Add(Timeout))
}

func TestWaitForUpgradeCheckRetainsDiagnosticAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	wantErr := context.Canceled
	wantDiagnostic := "COD rollout blocked by ownership conflict"
	attempts := 0

	err := WaitForUpgradeCheck(ctx, func(ctx context.Context) error {
		attempts++
		if attempts == 1 {
			return errors.New(wantDiagnostic)
		}
		cancel()
		return ctx.Err()
	})

	require.ErrorIs(t, err, wantErr)
	require.ErrorContains(t, err, wantDiagnostic)
}

func TestGetResourceHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	kind, name, namespace := "configmap", "test", "default"
	wantErr := context.Canceled

	preserveDiscoveryState(t)
	fakeCLI(t, "exit 1\n")
	cancel()
	_, err := GetResource(ctx, kind, name, namespace)

	require.ErrorIs(t, err, wantErr)
}
