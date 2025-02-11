package finalizers

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	crfinalizer "sigs.k8s.io/controller-runtime/pkg/finalizer"
)

type FinalizerFunc func(ctx context.Context, obj client.Object) (crfinalizer.Result, error)

func (f FinalizerFunc) Finalize(ctx context.Context, obj client.Object) (crfinalizer.Result, error) {
	return f(ctx, obj)
}
