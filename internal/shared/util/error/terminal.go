package error

import (
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// terminalErrorWithReason is an internal error type that carries a Reason field
// to provide more granular categorization of terminal errors for status conditions.
type terminalErrorWithReason struct {
	reason string
	err    error
}

func (e *terminalErrorWithReason) Error() string {
	return e.err.Error()
}

func (e *terminalErrorWithReason) Unwrap() error {
	return e.err
}

// NewTerminalError creates a terminal error with a specific reason.
// The error is wrapped with reconcile.TerminalError so controller-runtime
// recognizes it as terminal, while preserving the reason for status reporting.
//
// Example usage:
//
//	return error.NewTerminalError(ocv1.ReasonInvalidConfiguration, fmt.Errorf("missing required field"))
//
// The reason can be extracted later using ExtractTerminalReason() when setting
// status conditions to provide more specific feedback than just "Blocked".
func NewTerminalError(reason string, err error) error {
	return reconcile.TerminalError(&terminalErrorWithReason{
		reason: reason,
		err:    err,
	})
}

// ExtractTerminalReason extracts the reason from a terminal error created with
// NewTerminalError. Returns the reason and true if found, or empty string and
// false if the error was not created with NewTerminalError.
//
// This allows setStatusProgressing to use specific reasons like "InvalidConfiguration"
// instead of the generic "Blocked" for all terminal errors.
func ExtractTerminalReason(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	// Unwrap the reconcile.TerminalError wrapper first
	unwrapped := errors.Unwrap(err)
	var terr *terminalErrorWithReason
	if errors.As(unwrapped, &terr) {
		return terr.reason, true
	}
	return "", false
}

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
