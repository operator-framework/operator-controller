package testutil

import (
	"context"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

type FakeCatalogClient struct {
	bundles []*catalogmetadata.Bundle
	err     error
}

func NewFakeCatalogClient(b []*catalogmetadata.Bundle) FakeCatalogClient {
	return FakeCatalogClient{
		bundles: b,
	}
}

func NewFakeCatalogClientWithError(e error) FakeCatalogClient {
	return FakeCatalogClient{
		err: e,
	}
}

func (c *FakeCatalogClient) Bundles(_ context.Context) ([]*catalogmetadata.Bundle, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.bundles, nil
}
