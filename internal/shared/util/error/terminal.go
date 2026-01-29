package error

import (
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func WrapTerminal(err error, isTerminal bool) error {
	if !isTerminal || err == nil {
		return err
	}
	return reconcile.TerminalError(err)
}

// UnwrapTerminal unwraps a TerminalError to get the underlying error without
// the "terminal error:" prefix that reconcile.TerminalError adds to the message.
// This is useful when displaying error messages in status conditions where the
// terminal/blocked nature is already conveyed by the condition Reason field.
//
// If err is not a TerminalError, it returns err unchanged.
// If err is nil, it returns nil.
func UnwrapTerminal(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, reconcile.TerminalError(nil)) {
		if unwrapped := errors.Unwrap(err); unwrapped != nil {
			return unwrapped
		}
	}
	return err
}
