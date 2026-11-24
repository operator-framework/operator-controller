package error

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestNewTerminalError(t *testing.T) {
	inner := errors.New("boom")
	err := NewTerminalError("SomeReason", inner)

	require.ErrorIs(t, err, reconcile.TerminalError(nil), "error should be a terminal error")
	require.ErrorIs(t, err, inner, "error should wrap the inner error")
	require.Equal(t, "terminal error: boom", err.Error())

	reason, ok := ExtractTerminalReason(err)
	require.True(t, ok)
	require.Equal(t, "SomeReason", reason)
}

func TestExtractTerminalReason(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantReason string
		wantOK     bool
	}{
		{
			name: "nil error",
			err:  nil,
		},
		{
			name: "plain error",
			err:  errors.New("boom"),
		},
		{
			name: "terminal error without a reason",
			err:  reconcile.TerminalError(errors.New("boom")),
		},
		{
			name: "terminal error without a reason created by WrapTerminal",
			err:  WrapTerminal(errors.New("boom"), true),
		},
		{
			name:       "terminal error with a reason",
			err:        NewTerminalError("SomeReason", errors.New("boom")),
			wantReason: "SomeReason",
			wantOK:     true,
		},
		{
			name:       "terminal error with a reason wrapped by another error",
			err:        fmt.Errorf("context: %w", NewTerminalError("SomeReason", errors.New("boom"))),
			wantReason: "SomeReason",
			wantOK:     true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reason, ok := ExtractTerminalReason(tc.err)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantReason, reason)
		})
	}
}

func TestWrapTerminal(t *testing.T) {
	inner := errors.New("boom")

	t.Run("nil error stays nil when terminal", func(t *testing.T) {
		require.NoError(t, WrapTerminal(nil, true))
	})
	t.Run("nil error stays nil when not terminal", func(t *testing.T) {
		require.NoError(t, WrapTerminal(nil, false))
	})
	t.Run("non-terminal returns the original error", func(t *testing.T) {
		err := WrapTerminal(inner, false)
		require.Same(t, inner, err)
		require.NotErrorIs(t, err, reconcile.TerminalError(nil))
	})
	t.Run("terminal wraps the original error", func(t *testing.T) {
		err := WrapTerminal(inner, true)
		require.ErrorIs(t, err, reconcile.TerminalError(nil))
		require.ErrorIs(t, err, inner)
	})
}

func TestUnwrapTerminal(t *testing.T) {
	inner := errors.New("boom")

	tests := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "nil error",
			err:  nil,
			want: nil,
		},
		{
			name: "plain error is returned unchanged",
			err:  inner,
			want: inner,
		},
		{
			name: "terminal error returns the inner error",
			err:  reconcile.TerminalError(inner),
			want: inner,
		},
		{
			name: "terminal error with a reason returns the inner error",
			err:  NewTerminalError("SomeReason", inner),
			want: inner,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := UnwrapTerminal(tc.err)
			if tc.want == nil {
				require.NoError(t, got)
				return
			}
			require.Equal(t, tc.want.Error(), got.Error())
			require.NotErrorIs(t, got, reconcile.TerminalError(nil))
		})
	}
}
