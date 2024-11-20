package cache

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
)

var _ client.Cache = &filesystemCache{}

func NewFilesystemCache(cachePath string) *filesystemCache {
	return &filesystemCache{
		cachePath:              cachePath,
		mutex:                  sync.RWMutex{},
		cacheDataByCatalogName: map[string]cacheData{},
	}
}

// cacheData holds information about a catalog
// other than it's contents that is used for
// making decisions on when to attempt to refresh
// the cache.
type cacheData struct {
	Ref   string
	Error error
}

// FilesystemCache is a cache that
// uses the local filesystem for caching
// catalog contents.
type filesystemCache struct {
	mutex                  sync.RWMutex
	cachePath              string
	cacheDataByCatalogName map[string]cacheData
}

// Put writes content from source to the filesystem and stores errToCache
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
//
// This cache implementation tracks only one version of cache per catalog,
// so Put will override any existing cache on the filesystem for catalogName
// if resolvedRef does not match the one which is already tracked.
func (fsc *filesystemCache) Put(catalogName, resolvedRef string, source io.Reader, errToCache error) (fs.FS, error) {
	fsc.mutex.Lock()
	defer fsc.mutex.Unlock()

	var cacheFS fs.FS
	if errToCache == nil {
		cacheFS, errToCache = fsc.writeFS(catalogName, source)
	}
	fsc.cacheDataByCatalogName[catalogName] = cacheData{
		Ref:   resolvedRef,
		Error: errToCache,
	}

	return cacheFS, errToCache
}

func (fsc *filesystemCache) writeFS(catalogName string, source io.Reader) (fs.FS, error) {
	cacheDir := fsc.cacheDir(catalogName)

	tmpDir, err := os.MkdirTemp(fsc.cachePath, fmt.Sprintf(".%s-", catalogName))
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory to unpack catalog metadata: %v", err)
	}

	if err := declcfg.WalkMetasReader(source, func(meta *declcfg.Meta, err error) error {
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

	return os.DirFS(cacheDir), nil
}

// Get returns cache for a specified catalog name and version (resolvedRef).
//
// Method behaviour is as follows:
//   - If cache exists, it returns a non-nil fs.FS and nil error
//   - If cache doesn't exist, it returns nil fs.FS and nil error
//   - If there was an error during cache population,
//     it returns nil fs.FS and the error from the cache population.
//     In other words - cache population errors are also cached.
func (fsc *filesystemCache) Get(catalogName, resolvedRef string) (fs.FS, error) {
	fsc.mutex.RLock()
	defer fsc.mutex.RUnlock()
	return fsc.get(catalogName, resolvedRef)
}

func (fsc *filesystemCache) get(catalogName, resolvedRef string) (fs.FS, error) {
	cacheDir := fsc.cacheDir(catalogName)
	if data, ok := fsc.cacheDataByCatalogName[catalogName]; ok {
		if resolvedRef == data.Ref {
			if data.Error != nil {
				return nil, data.Error
			}
			return os.DirFS(cacheDir), nil
		}
	}

	return nil, nil
}

// Remove deletes cache directory for a given catalog from the filesystem
func (fsc *filesystemCache) Remove(catalogName string) error {
	cacheDir := fsc.cacheDir(catalogName)

	fsc.mutex.Lock()
	defer fsc.mutex.Unlock()

	if _, exists := fsc.cacheDataByCatalogName[catalogName]; !exists {
		return nil
	}

	if err := os.RemoveAll(cacheDir); err != nil {
		return fmt.Errorf("error removing cache directory: %v", err)
	}

	delete(fsc.cacheDataByCatalogName, catalogName)
	return nil
}

func (fsc *filesystemCache) cacheDir(catalogName string) string {
	return filepath.Join(fsc.cachePath, catalogName)
}
