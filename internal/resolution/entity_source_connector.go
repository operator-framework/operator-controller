package resolution

import (
	"context"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/deppy/pkg/deppy/input"
)

type EntitySourceConnector interface {
	ListEntities(ctx context.Context, catsrc *v1alpha1.CatalogSource) ([]*input.Entity, error)
}
