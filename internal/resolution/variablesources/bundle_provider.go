package variablesources

import (
	"context"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

// BundleProvider provides the Bundles method through which we can retrieve
// a list of Bundles from any source, generally from a catalog client of
// some kind.
type BundleProvider interface {
	Bundles(ctx context.Context) ([]*catalogmetadata.Bundle, error)
}
