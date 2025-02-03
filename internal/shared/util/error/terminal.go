package error

import "sigs.k8s.io/controller-runtime/pkg/reconcile"

func WrapTerminal(err error, isTerminal bool) error {
	if !isTerminal || err == nil {
		return err
	}
	return reconcile.TerminalError(err)
}
