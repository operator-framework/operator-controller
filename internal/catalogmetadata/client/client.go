package client

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

func NewClient(cl client.Client) *Client {
	return &Client{cl: cl}
}

// Client is reading catalog metadata
type Client struct {
	// Note that eventually we will be reading from catalogd http API
	// instead of kube API server. We will need to swap this implementation.
	cl client.Client
}

func (c *Client) Bundles(ctx context.Context) ([]*catalogmetadata.Bundle, error) {
	var allBundles []*catalogmetadata.Bundle

	var catalogList catalogd.CatalogList
	if err := c.cl.List(ctx, &catalogList); err != nil {
		return nil, err
	}
	for _, catalog := range catalogList.Items {
		channels, err := fetchCatalogMetadata[catalogmetadata.Channel](ctx, c.cl, catalog.Name, declcfg.SchemaChannel)
		if err != nil {
			return nil, err
		}

		bundles, err := fetchCatalogMetadata[catalogmetadata.Bundle](ctx, c.cl, catalog.Name, declcfg.SchemaBundle)
		if err != nil {
			return nil, err
		}

		bundles, err = populateExtraFields(catalog.Name, channels, bundles)
		if err != nil {
			return nil, err
		}

		allBundles = append(allBundles, bundles...)
	}

	return allBundles, nil
}

func fetchCatalogMetadata[T catalogmetadata.Schemas](ctx context.Context, cl client.Client, catalogName, schema string) ([]*T, error) {
	var cmList catalogd.CatalogMetadataList
	err := cl.List(ctx, &cmList, client.MatchingLabels{"catalog": catalogName, "schema": schema})
	if err != nil {
		return nil, err
	}

	content, err := catalogmetadata.Unmarshal[T](cmList.Items)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling catalog metadata: %s", err)
	}

	return content, nil
}

func populateExtraFields(catalogName string, channels []*catalogmetadata.Channel, bundles []*catalogmetadata.Bundle) ([]*catalogmetadata.Bundle, error) {
	bundlesMap := map[string]*catalogmetadata.Bundle{}
	for i := range bundles {
		bundleKey := fmt.Sprintf("%s-%s", bundles[i].Package, bundles[i].Name)
		bundlesMap[bundleKey] = bundles[i]

		bundles[i].CatalogName = catalogName
	}

	for _, ch := range channels {
		for _, chEntry := range ch.Entries {
			bundleKey := fmt.Sprintf("%s-%s", ch.Package, chEntry.Name)
			bundle, ok := bundlesMap[bundleKey]
			if !ok {
				return nil, fmt.Errorf("bundle %q not found in catalog %q (package %q, channel %q)", chEntry.Name, catalogName, ch.Package, ch.Name)
			}

			bundle.InChannels = append(bundle.InChannels, ch)
		}
	}

	return bundles, nil
}
