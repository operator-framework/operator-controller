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
		{
			// ExtractTerminalReason only unwraps one level, so a terminal error
			// hidden behind errors.Join is not found.
			name: "terminal error with a reason joined with another error",
			err:  errors.Join(errors.New("other"), NewTerminalError("SomeReason", errors.New("boom"))),
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
	t.Run("terminal wraps an error that already wraps another error", func(t *testing.T) {
		wrapped := fmt.Errorf("context: %w", inner)
		err := WrapTerminal(wrapped, true)
		require.ErrorIs(t, err, reconcile.TerminalError(nil))
		require.ErrorIs(t, err, inner)
		require.Equal(t, "terminal error: context: boom", err.Error())
	})
	t.Run("non-terminal leaves a wrapped terminal error terminal", func(t *testing.T) {
		wrapped := fmt.Errorf("context: %w", reconcile.TerminalError(inner))
		err := WrapTerminal(wrapped, false)
		require.Same(t, wrapped, err)
		require.ErrorIs(t, err, reconcile.TerminalError(nil))
	})
}

func TestUnwrapTerminal(t *testing.T) {
	inner := errors.New("boom")

	tests := []struct {
		name string
		err  error
		want error
		// wantTerminal is true when the returned error is expected to still be
		// a terminal error.
		wantTerminal bool
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
		{
			// UnwrapTerminal sees the terminal error through the wrapper, but
			// only strips one level: the wrapper is removed and the terminal
			// error underneath is returned as is.
			name:         "error wrapping a terminal error returns the terminal error",
			err:          fmt.Errorf("context: %w", reconcile.TerminalError(inner)),
			want:         reconcile.TerminalError(inner),
			wantTerminal: true,
		},
		{
			name:         "error wrapping a terminal error with a reason returns the terminal error",
			err:          fmt.Errorf("context: %w", NewTerminalError("SomeReason", inner)),
			want:         NewTerminalError("SomeReason", inner),
			wantTerminal: true,
		},
		{
			// errors.Join has no single underlying error, so the joined error
			// is returned unchanged.
			name:         "joined error containing a terminal error is returned unchanged",
			err:          errors.Join(errors.New("other"), reconcile.TerminalError(inner)),
			want:         errors.Join(errors.New("other"), reconcile.TerminalError(inner)),
			wantTerminal: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := UnwrapTerminal(tc.err)
			if tc.want == nil {
				require.NoError(t, got)
				return
			}
			require.EqualError(t, got, tc.want.Error())
			if tc.wantTerminal {
				require.ErrorIs(t, got, reconcile.TerminalError(nil))
			} else {
				require.NotErrorIs(t, got, reconcile.TerminalError(nil))
			}
		})
	}
}
