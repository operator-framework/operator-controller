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
	FetchCatalogContents(ctx context.Context, catalog *catalogd.ClusterCatalog) (io.ReadCloser, error)
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

func (c *Client) Bundles(ctx context.Context) ([]*catalogmetadata.Bundle, error) {
	var allBundles []*catalogmetadata.Bundle

	var catalogList catalogd.ClusterCatalogList
	if err := c.cl.List(ctx, &catalogList); err != nil {
		return nil, err
	}
	for _, catalog := range catalogList.Items {
		// if the catalog has not been successfully unpacked, skip it
		if !meta.IsStatusConditionPresentAndEqual(catalog.Status.Conditions, catalogd.TypeUnpacked, metav1.ConditionTrue) {
			continue
		}
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
			case declcfg.SchemaChannel:
				var content catalogmetadata.Channel
				if err := json.Unmarshal(meta.Blob, &content); err != nil {
					return fmt.Errorf("error unmarshalling channel from catalog metadata: %s", err)
				}
				channels = append(channels, &content)
			case declcfg.SchemaBundle:
				var content catalogmetadata.Bundle
				if err := json.Unmarshal(meta.Blob, &content); err != nil {
					return fmt.Errorf("error unmarshalling bundle from catalog metadata: %s", err)
				}
				bundles = append(bundles, &content)
			case declcfg.SchemaDeprecation:
				var content catalogmetadata.Deprecation
				if err := json.Unmarshal(meta.Blob, &content); err != nil {
					return fmt.Errorf("error unmarshalling deprecation from catalog metadata: %s", err)
				}
				deprecations = append(deprecations, &content)
			}

			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("error processing response: %s", err)
		}

		bundles, err = PopulateExtraFields(catalog.Name, channels, bundles, deprecations)
		if err != nil {
			return nil, err
		}

		allBundles = append(allBundles, bundles...)
	}

	return allBundles, nil
}

func PopulateExtraFields(catalogName string, channels []*catalogmetadata.Channel, bundles []*catalogmetadata.Bundle, deprecations []*catalogmetadata.Deprecation) ([]*catalogmetadata.Bundle, error) {
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

	// According to https://docs.google.com/document/d/1EzefSzoGZL2ipBt-eCQwqqNwlpOIt7wuwjG6_8ZCi5s/edit?usp=sharing
	// the olm.deprecations FBC object is only valid when either 0 or 1 instances exist
	// for any given package
	deprecationMap := make(map[string]*catalogmetadata.Deprecation, len(deprecations))
	for _, deprecation := range deprecations {
		deprecationMap[deprecation.Package] = deprecation
	}

	for i := range bundles {
		if dep, ok := deprecationMap[bundles[i].Package]; ok {
			for _, entry := range dep.Entries {
				switch entry.Reference.Schema {
				case declcfg.SchemaPackage:
					bundles[i].Deprecations = append(bundles[i].Deprecations, entry)
				case declcfg.SchemaChannel:
					for _, ch := range bundles[i].InChannels {
						if ch.Name == entry.Reference.Name {
							bundles[i].Deprecations = append(bundles[i].Deprecations, entry)
							break
						}
					}
				case declcfg.SchemaBundle:
					if bundles[i].Name == entry.Reference.Name {
						bundles[i].Deprecations = append(bundles[i].Deprecations, entry)
					}
				}
			}
		}
	}

	return bundles, nil
}
