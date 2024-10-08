package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

type Cache interface {
	// Get returns cache for a specified catalog name and version (resolvedRef).
	//
	// Method behaviour is as follows:
	//   - If cache exists, it returns a non-nil fs.FS and nil error
	//   - If cache doesn't exist, it returns nil fs.FS and nil error
	//   - If there was an error during cache population,
	//     it returns nil fs.FS and the error from the cache population.
	//     In other words - cache population errors are also cached.
	Get(catalogName, resolvedRef string) (fs.FS, error)

	// Put writes content from source or from errToCache in the cache backend
	// for a specified catalog name and version (resolvedRef).
	//
	// Method behaviour is as follows:
	//   - If successfully populated cache for catalogName and resolvedRef exists,
	//     errToCache is ignored and existing cache returned with nil error
	//   - If existing cache for catalogName and resolvedRef exists but
	//     is populated with an error, update the cache with either
	//     new content from source or errToCache.
	//   - If cache doesn't exist, populate it with either new content
	//     from source or errToCache.
	Put(catalogName, resolvedRef string, source io.Reader, errToCache error) (fs.FS, error)
}

func New(cache Cache, httpClient func() (*http.Client, error)) *Client {
	return &Client{
		cache:      cache,
		httpClient: httpClient,
	}
}

// Client is reading catalog metadata
type Client struct {
	cache      Cache
	httpClient func() (*http.Client, error)
}

func (c *Client) GetPackage(ctx context.Context, catalog *catalogd.ClusterCatalog, pkgName string) (*declcfg.DeclarativeConfig, error) {
	if err := validateCatalog(catalog); err != nil {
		return nil, err
	}

	catalogFsys, err := c.cache.Get(catalog.Name, catalog.Status.ResolvedSource.Image.Ref)
	if err != nil {
		return nil, fmt.Errorf("error retrieving catalog cache: %v", err)
	}
	if catalogFsys == nil {
		// TODO: https://github.com/operator-framework/operator-controller/pull/1284
		// For now we are still populating cache (if absent) on-demand,
		// but we might end up just returning a "cache not found" error here
		// once we implement cache population in the controller.
		catalogFsys, err = c.PopulateCache(ctx, catalog)
		if err != nil {
			return nil, fmt.Errorf("error fetching catalog contents: %v", err)
		}
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

func (c *Client) PopulateCache(ctx context.Context, catalog *catalogd.ClusterCatalog) (fs.FS, error) {
	if err := validateCatalog(catalog); err != nil {
		return nil, err
	}

	resp, err := c.doRequest(ctx, catalog)
	if err != nil {
		// Any errors from the http request we want to cache
		// so later on cache get they can be bubbled up to the user.
		return c.cache.Put(catalog.Name, catalog.Status.ResolvedSource.Image.Ref, nil, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errToCache := fmt.Errorf("error: received unexpected response status code %d", resp.StatusCode)
		return c.cache.Put(catalog.Name, catalog.Status.ResolvedSource.Image.Ref, nil, errToCache)
	}

	return c.cache.Put(catalog.Name, catalog.Status.ResolvedSource.Image.Ref, resp.Body, nil)
}

func (c *Client) doRequest(ctx context.Context, catalog *catalogd.ClusterCatalog) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, catalog.Status.ContentURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error forming request: %v", err)
	}

	client, err := c.httpClient()
	if err != nil {
		return nil, fmt.Errorf("error getting HTTP client: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error performing request: %v", err)
	}

	return resp, nil
}

func validateCatalog(catalog *catalogd.ClusterCatalog) error {
	if catalog == nil {
		return fmt.Errorf("error: provided catalog must be non-nil")
	}

	// if the catalog is not being served, report an error. This ensures that our
	// reconciles are deterministic and wait for all desired catalogs to be ready.
	if !meta.IsStatusConditionPresentAndEqual(catalog.Status.Conditions, catalogd.TypeServing, metav1.ConditionTrue) {
		return fmt.Errorf("catalog %q is not being served", catalog.Name)
	}

	if catalog.Status.ResolvedSource == nil {
		return fmt.Errorf("error: catalog %q has a nil status.resolvedSource value", catalog.Name)
	}

	if catalog.Status.ResolvedSource.Image == nil {
		return fmt.Errorf("error: catalog %q has a nil status.resolvedSource.image value", catalog.Name)
	}

	return nil
}
