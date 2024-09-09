package client

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

// Fetcher is an interface to facilitate fetching
// catalog contents from catalogd.
type Fetcher interface {
	// FetchCatalogContents fetches contents from the catalogd HTTP
	// server for the catalog provided. It returns a fs.FS containing the FBC contents.
	// Each sub directory contains FBC for a single package
	// and the directory name is package name.
	// Returns an error if any occur.
	FetchCatalogContents(ctx context.Context, catalog *catalogd.ClusterCatalog) (fs.FS, error)
}

func New(fetcher Fetcher) *Client {
	return &Client{
		fetcher: fetcher,
	}
}

// Client is reading catalog metadata
type Client struct {
	// fetcher is the Fetcher to use for fetching catalog contents
	fetcher Fetcher
}

func (c *Client) GetPackage(ctx context.Context, catalog *catalogd.ClusterCatalog, pkgName string) (*declcfg.DeclarativeConfig, error) {
	// if the catalog has not been successfully unpacked, report an error. This ensures that our
	// reconciles are deterministic and wait for all desired catalogs to be ready.
	if !meta.IsStatusConditionPresentAndEqual(catalog.Status.Conditions, catalogd.TypeUnpacked, metav1.ConditionTrue) {
		return nil, fmt.Errorf("catalog %q is not unpacked", catalog.Name)
	}

	catalogFsys, err := c.fetcher.FetchCatalogContents(ctx, catalog)
	if err != nil {
		return nil, fmt.Errorf("error fetching catalog contents: %v", err)
	}

	pkgFsys, err := fs.Sub(catalogFsys, pkgName)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("error getting package %q: %v", pkgName, err)
		}
		return &declcfg.DeclarativeConfig{}, nil
	}

	pkgFBC, err := declcfg.LoadFS(ctx, pkgFsys)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("error loading package %q: %v", pkgName, err)
		}
		return &declcfg.DeclarativeConfig{}, nil
	}
	return pkgFBC, nil
}
