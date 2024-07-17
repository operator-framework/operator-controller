package cache

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
)

var _ client.Fetcher = &filesystemCache{}

// NewFilesystemCache returns a client.Fetcher implementation that uses a
// local filesystem to cache Catalog contents. When fetching the Catalog contents
// it will:
// - Check if the Catalog is cached
//   - IF !cached it will fetch from the catalogd HTTP server and cache the response
//   - IF cached it will verify the cache is up to date. If it is up to date it will return
//     the cached contents, if not it will fetch the new contents from the catalogd HTTP
//     server and update the cached contents.
func NewFilesystemCache(cachePath string, clientFunc func() (*http.Client, error)) client.Fetcher {
	return &filesystemCache{
		cachePath:              cachePath,
		mutex:                  sync.RWMutex{},
		getClient:              clientFunc,
		cacheDataByCatalogName: map[string]cacheData{},
	}
}

// cacheData holds information about a catalog
// other than it's contents that is used for
// making decisions on when to attempt to refresh
// the cache.
type cacheData struct {
	ResolvedRef string
}

// FilesystemCache is a cache that
// uses the local filesystem for caching
// catalog contents. It will fetch catalog
// contents if the catalog does not already
// exist in the cache.
type filesystemCache struct {
	mutex                  sync.RWMutex
	cachePath              string
	getClient              func() (*http.Client, error)
	cacheDataByCatalogName map[string]cacheData
}

// FetchCatalogContents implements the client.Fetcher interface and
// will fetch the contents for the provided Catalog from the filesystem.
// If the provided Catalog has not yet been cached, it will make a GET
// request to the Catalogd HTTP server to get the Catalog contents and cache
// them. The cache will be updated automatically if a Catalog is noticed to
// have a different resolved image reference.
// The Catalog provided to this function is expected to:
// - Be non-nil
// - Have a non-nil Catalog.Status.ResolvedSource.Image
// This ensures that we are only attempting to fetch catalog contents for Catalog
// resources that have been successfully reconciled, unpacked, and are being served.
// These requirements help ensure that we can rely on status conditions to determine
// when to issue a request to update the cached Catalog contents.
func (fsc *filesystemCache) FetchCatalogContents(ctx context.Context, catalog *catalogd.ClusterCatalog) (fs.FS, error) {
	if catalog == nil {
		return nil, fmt.Errorf("error: provided catalog must be non-nil")
	}

	if catalog.Status.ResolvedSource == nil {
		return nil, fmt.Errorf("error: catalog %q has a nil status.resolvedSource value", catalog.Name)
	}

	if catalog.Status.ResolvedSource.Image == nil {
		return nil, fmt.Errorf("error: catalog %q has a nil status.resolvedSource.image value", catalog.Name)
	}

	cacheDir := filepath.Join(fsc.cachePath, catalog.Name)
	fsc.mutex.RLock()
	if data, ok := fsc.cacheDataByCatalogName[catalog.Name]; ok {
		if catalog.Status.ResolvedSource.Image.ResolvedRef == data.ResolvedRef {
			fsc.mutex.RUnlock()
			return os.DirFS(cacheDir), nil
		}
	}
	fsc.mutex.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, catalog.Status.ContentURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error forming request: %v", err)
	}

	client, err := fsc.getClient()
	if err != nil {
		return nil, fmt.Errorf("error getting HTTP client: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error performing request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error: received unexpected response status code %d", resp.StatusCode)
	}

	fsc.mutex.Lock()
	defer fsc.mutex.Unlock()

	// make sure we only write if this info hasn't been updated
	// by another thread. The check here, if multiple threads are
	// updating this, has no way to tell if the current ref is the
	// newest possible ref. If another thread has already updated
	// this to be the same value, skip the write logic and return
	// the cached contents
	if data, ok := fsc.cacheDataByCatalogName[catalog.Name]; ok {
		if data.ResolvedRef == catalog.Status.ResolvedSource.Image.Ref {
			return os.DirFS(cacheDir), nil
		}
	}

	tmpDir, err := os.MkdirTemp(fsc.cachePath, fmt.Sprintf(".%s-", catalog.Name))
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory to unpack catalog metadata: %v", err)
	}

	if err := declcfg.WalkMetasReader(resp.Body, func(meta *declcfg.Meta, err error) error {
		if err != nil {
			return fmt.Errorf("error parsing catalog contents: %v", err)
		}
		pkgName := meta.Package
		if meta.Schema == declcfg.SchemaPackage {
			pkgName = meta.Name
		}
		metaName := meta.Name
		if meta.Name == "" {
			metaName = meta.Schema
		}
		metaPath := filepath.Join(tmpDir, pkgName, meta.Schema, metaName+".json")
		if err := os.MkdirAll(filepath.Dir(metaPath), os.ModePerm); err != nil {
			return fmt.Errorf("error creating directory for catalog metadata: %v", err)
		}
		if err := os.WriteFile(metaPath, meta.Blob, 0600); err != nil {
			return fmt.Errorf("error writing catalog metadata to file: %v", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := os.RemoveAll(cacheDir); err != nil {
		return nil, fmt.Errorf("error removing old cache directory: %v", err)
	}
	if err := os.Rename(tmpDir, cacheDir); err != nil {
		return nil, fmt.Errorf("error moving temporary directory to cache directory: %v", err)
	}

	fsc.cacheDataByCatalogName[catalog.Name] = cacheData{
		ResolvedRef: catalog.Status.ResolvedSource.Image.ResolvedRef,
	}

	return os.DirFS(cacheDir), nil
}
