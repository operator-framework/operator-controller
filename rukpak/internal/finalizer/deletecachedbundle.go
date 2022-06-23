package finalizer

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/finalizer"

	"github.com/operator-framework/rukpak/internal/storage"
)

var _ finalizer.Finalizer = &DeleteCachedBundle{}

const DeleteCachedBundleKey = "core.rukpak.io/delete-cached-bundle"

type DeleteCachedBundle struct {
	storage.Storage
}

func (f DeleteCachedBundle) Finalize(ctx context.Context, obj client.Object) (finalizer.Result, error) {
	return finalizer.Result{}, f.Delete(ctx, obj)
}
