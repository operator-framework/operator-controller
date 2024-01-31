package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

// Fetcher is an interface to facilitate fetching
// catalog contents from catalogd.
type Fetcher interface {
	// FetchCatalogContents fetches contents from the catalogd HTTP
	// server for the catalog provided. It returns an io.ReadCloser
	// containing the FBC contents that the caller is expected to close.
	// returns an error if any occur.
	FetchCatalogContents(ctx context.Context, catalog *catalogd.Catalog) (io.ReadCloser, error)
}

func New(cl client.Client, fetcher Fetcher) *Client {
	return &Client{
		cl:      cl,
		fetcher: fetcher,
	}
}

// Client is reading catalog metadata
type Client struct {
	// Note that eventually we will be reading from catalogd http API
	// instead of kube API server. We will need to swap this implementation.
	cl client.Client

	// fetcher is the Fetcher to use for fetching catalog contents
	fetcher Fetcher
}

type Contents struct {
	Packages []*catalogmetadata.Package
	Channels []*catalogmetadata.Channel
	Bundles  []*catalogmetadata.Bundle
}

func (c *Client) CatalogContents(ctx context.Context) (*Contents, error) {
	var catalogList catalogd.CatalogList
	if err := c.cl.List(ctx, &catalogList); err != nil {
		return nil, err
	}

	contents := &Contents{
		Packages: []*catalogmetadata.Package{},
		Channels: []*catalogmetadata.Channel{},
		Bundles:  []*catalogmetadata.Bundle{},
	}

	for _, catalog := range catalogList.Items {
		// if the catalog has not been successfully unpacked, skip it
		if !meta.IsStatusConditionPresentAndEqual(catalog.Status.Conditions, catalogd.TypeUnpacked, metav1.ConditionTrue) {
			continue
		}

		packages := []*catalogmetadata.Package{}
		channels := []*catalogmetadata.Channel{}
		bundles := []*catalogmetadata.Bundle{}
		deprecations := []*catalogmetadata.Deprecation{}

		rc, err := c.fetcher.FetchCatalogContents(ctx, catalog.DeepCopy())
		if err != nil {
			return nil, fmt.Errorf("error fetching catalog contents: %s", err)
		}
		defer rc.Close()

		err = declcfg.WalkMetasReader(rc, func(meta *declcfg.Meta, err error) error {
			if err != nil {
				return fmt.Errorf("error was provided to the WalkMetasReaderFunc: %s", err)
			}
			switch meta.Schema {
			case declcfg.SchemaPackage:
				var content catalogmetadata.Package
				if err := json.Unmarshal(meta.Blob, &content); err != nil {
					return fmt.Errorf("error unmarshalling channel from catalog metadata: %s", err)
				}
				content.Catalog = catalog.Name
				packages = append(packages, &content)
			case declcfg.SchemaChannel:
				var content catalogmetadata.Channel
				if err := json.Unmarshal(meta.Blob, &content); err != nil {
					return fmt.Errorf("error unmarshalling channel from catalog metadata: %s", err)
				}
				content.Catalog = catalog.Name
				channels = append(channels, &content)
			case declcfg.SchemaBundle:
				var content catalogmetadata.Bundle
				if err := json.Unmarshal(meta.Blob, &content); err != nil {
					return fmt.Errorf("error unmarshalling bundle from catalog metadata: %s", err)
				}
				content.Catalog = catalog.Name
				bundles = append(bundles, &content)
			case declcfg.SchemaDeprecation:
				var content catalogmetadata.Deprecation
				if err := json.Unmarshal(meta.Blob, &content); err != nil {
					return fmt.Errorf("error unmarshalling deprecation from catalog metadata: %s", err)
				}
				content.Catalog = catalog.Name
				deprecations = append(deprecations, &content)
			}

			return nil
		})

		if err != nil {
			return nil, fmt.Errorf("error processing response: %s", err)
		}

		for _, deprecation := range deprecations {
			for _, entry := range deprecation.Entries {
				switch entry.Reference.Schema {
				case declcfg.SchemaPackage:
					for _, pkg := range packages {
						if pkg.Name == deprecation.Package {
							pkg.Deprecation = &declcfg.DeprecationEntry{
								Reference: entry.Reference,
								Message:   entry.Message,
							}
						}
					}
				case declcfg.SchemaChannel:
					for _, channel := range channels {
						if channel.Package == deprecation.Package && channel.Name == entry.Reference.Name {
							channel.Deprecation = &declcfg.DeprecationEntry{
								Reference: entry.Reference,
								Message:   entry.Message,
							}
						}
					}
				case declcfg.SchemaBundle:
					for _, bundle := range bundles {
						if bundle.Package == deprecation.Package && bundle.Name == entry.Reference.Name {
							bundle.Deprecation = &declcfg.DeprecationEntry{
								Reference: entry.Reference,
								Message:   entry.Message,
							}
						}
					}
				}
			}
		}

		contents.Packages = append(contents.Packages, packages...)
		contents.Channels = append(contents.Channels, channels...)
		contents.Bundles = append(contents.Bundles, bundles...)
	}

	return contents, nil
}
