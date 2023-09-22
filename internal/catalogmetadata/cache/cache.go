package cache

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"

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
func NewFilesystemCache(cachePath string, client *http.Client) client.Fetcher {
	return &filesystemCache{
		cachePath:              cachePath,
		mutex:                  sync.RWMutex{},
		client:                 client,
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
	client                 *http.Client
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
func (fsc *filesystemCache) FetchCatalogContents(ctx context.Context, catalog *catalogd.Catalog) (io.ReadCloser, error) {
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
	cacheFilePath := filepath.Join(cacheDir, "data.json")

	fsc.mutex.RLock()
	if data, ok := fsc.cacheDataByCatalogName[catalog.Name]; ok {
		if catalog.Status.ResolvedSource.Image.Ref == data.ResolvedRef {
			fsc.mutex.RUnlock()
			return os.Open(cacheFilePath)
		}
	}
	fsc.mutex.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, catalog.Status.ContentURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error forming request: %s", err)
	}

	resp, err := fsc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error performing request: %s", err)
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
			return os.Open(cacheFilePath)
		}
	}

	if err = os.MkdirAll(cacheDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("error creating cache directory for Catalog %q: %s", catalog.Name, err)
	}

	file, err := os.Create(cacheFilePath)
	if err != nil {
		return nil, fmt.Errorf("error creating cache file for Catalog %q: %s", catalog.Name, err)
	}

	if _, err := io.Copy(file, resp.Body); err != nil {
		return nil, fmt.Errorf("error writing contents to cache file for Catalog %q: %s", catalog.Name, err)
	}

	if err = file.Sync(); err != nil {
		return nil, fmt.Errorf("error syncing contents to cache file for Catalog %q: %s", catalog.Name, err)
	}

	if _, err = file.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("error resetting offset for cache file reader for Catalog %q: %s", catalog.Name, err)
	}

	fsc.cacheDataByCatalogName[catalog.Name] = cacheData{
		ResolvedRef: catalog.Status.ResolvedSource.Image.Ref,
	}

	return file, nil
}
