package catalogsource

import (
	"context"

	catsrcV1 "github.com/operator-framework/catalogd/pkg/apis/core/v1beta1"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CatsrcV1 interface {
	ListEntities(ctx context.Context, catsrc *catsrcV1.CatalogSource) ([]*input.Entity, error)
}

type catsrvV1beta1Connector struct {
	client client.Client
}

type Options struct {
	packageName string
}

func NewCatsrcConnector(cl client.Client, pacakges string) CatsrcV1 {
	return &catsrvV1beta1Connector{client: cl}
}

func (c *catsrvV1beta1Connector) ListEntities(ctx context.Context, catalogSource *catsrcV1.CatalogSource) ([]*input.Entity, error) {
	return nil, nil
}
