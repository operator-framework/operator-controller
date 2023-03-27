package catalogsource

import (
	"context"

	catsrcv1beta1 "github.com/anik120/rukpak-packageserver/pkg/apis/core/v1beta1"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type newcatsrcClient struct {
	ctx    context.Context
	client client.Client
}

func NewCatSrcClient(ctx context.Context, cl client.Client) RegistryClient[catsrcv1beta1.CatalogSource] {
	return newcatsrcClient{ctx: ctx, client: cl}
}

func (c newcatsrcClient) ListEntities(ctx context.Context, catsrc *catsrcv1beta1.CatalogSource) ([]*input.Entity, error) {
	// TODO: implement
	return nil, nil
}
