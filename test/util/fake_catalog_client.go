package testutil

import (
	"context"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
)

type FakeCatalogClient struct {
	bundles  []*catalogmetadata.Bundle
	channels []*catalogmetadata.Channel
	packages []*catalogmetadata.Package
	err      error
}

func NewFakeCatalogClient(b []*catalogmetadata.Bundle, c []*catalogmetadata.Channel, p []*catalogmetadata.Package) FakeCatalogClient {
	return FakeCatalogClient{
		bundles:  b,
		channels: c,
		packages: p,
	}
}

func NewFakeCatalogClientWithError(e error) FakeCatalogClient {
	return FakeCatalogClient{
		err: e,
	}
}

func (c *FakeCatalogClient) CatalogContents(_ context.Context) (*client.Contents, error) {
	if c.err != nil {
		return nil, c.err
	}
	return &client.Contents{Bundles: c.bundles, Channels: c.channels, Packages: c.packages}, nil
}
