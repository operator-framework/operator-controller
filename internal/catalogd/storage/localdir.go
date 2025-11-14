package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"github.com/operator-framework/operator-controller/internal/catalogd/server"
	"github.com/operator-framework/operator-controller/internal/catalogd/service"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

// LocalDirV1 is a storage Instance. When Storing a new FBC contained in
// fs.FS, the content is first written to a temporary file, after which
// it is copied to its final destination in RootDir/<catalogName>.jsonl. This is
// done so that clients accessing the content stored in RootDir/<catalogName>.json1
// have an atomic view of the content for a catalog.
type LocalDirV1 struct {
	RootDir            string
	RootURL            *url.URL
	EnableMetasHandler bool

	m sync.RWMutex
	// this singleflight Group is used in `getIndex()` to handle concurrent HTTP requests
	// optimally. With the use of this singleflight group, the index is loaded from disk
	// once per concurrent group of HTTP requests being handled by the metas handler.
	// The single flight instance gives us a way to load the index from disk exactly once
	// per concurrent group of callers, and then let every concurrent caller have access to
	// the loaded index. This avoids lots of unnecessary open/decode/close cycles when concurrent
	// requests are being handled, which improves overall performance and decreases response latency.
	sf singleflight.Group

	// GraphQL service for handling schema generation and caching
	graphqlSvc service.GraphQLService
}

var (
	_ Instance = (*LocalDirV1)(nil)
)

// NewLocalDirV1 creates a new LocalDirV1 storage instance
func NewLocalDirV1(rootDir string, rootURL *url.URL, enableMetasHandler bool) *LocalDirV1 {
	return &LocalDirV1{
		RootDir:            rootDir,
		RootURL:            rootURL,
		EnableMetasHandler: enableMetasHandler,
		graphqlSvc:         service.NewCachedGraphQLService(),
	}
}

func (s *LocalDirV1) Store(ctx context.Context, catalog string, fsys fs.FS) error {
	s.m.Lock()
	defer s.m.Unlock()

	if err := os.MkdirAll(s.RootDir, 0700); err != nil {
		return err
	}
	tmpCatalogDir, err := os.MkdirTemp(s.RootDir, fmt.Sprintf(".%s-*", catalog))
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpCatalogDir)

	storeMetaFuncs := []storeMetasFunc{storeCatalogData}
	if s.EnableMetasHandler {
		storeMetaFuncs = append(storeMetaFuncs, storeIndexData)
	}

	eg, egCtx := errgroup.WithContext(ctx)
	// Pre-allocate metaChans with correct capacity to avoid reallocation
	metaChans := make([]chan *declcfg.Meta, 0, len(storeMetaFuncs))

	for range storeMetaFuncs {
		metaChans = append(metaChans, make(chan *declcfg.Meta, 1))
	}
	for i, f := range storeMetaFuncs {
		eg.Go(func() error {
			return f(tmpCatalogDir, metaChans[i])
		})
	}
	err = declcfg.WalkMetasFS(egCtx, fsys, func(path string, meta *declcfg.Meta, err error) error {
		if err != nil {
			return err
		}
		for _, ch := range metaChans {
			select {
			case ch <- meta:
			case <-egCtx.Done():
				return egCtx.Err()
			}
		}
		return nil
	}, declcfg.WithConcurrency(1))
	for _, ch := range metaChans {
		close(ch)
	}
	if err != nil {
		return fmt.Errorf("error walking FBC root: %w", err)
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	catalogDir := s.catalogDir(catalog)
	err = errors.Join(
		os.RemoveAll(catalogDir),
		os.Rename(tmpCatalogDir, catalogDir),
	)
	if err != nil {
		return err
	}

	// Invalidate GraphQL schema cache for this catalog
	s.graphqlSvc.InvalidateCache(catalog)

	return nil
}

func (s *LocalDirV1) Delete(catalog string) error {
	s.m.Lock()
	defer s.m.Unlock()

	// Invalidate GraphQL cache
	s.graphqlSvc.InvalidateCache(catalog)

	return os.RemoveAll(s.catalogDir(catalog))
}

func (s *LocalDirV1) ContentExists(catalog string) bool {
	s.m.RLock()
	defer s.m.RUnlock()

	catalogFileStat, err := os.Stat(catalogFilePath(s.catalogDir(catalog)))
	if err != nil {
		return false
	}
	if !catalogFileStat.Mode().IsRegular() {
		// path is not valid content
		return false
	}

	if s.EnableMetasHandler {
		indexFileStat, err := os.Stat(catalogIndexFilePath(s.catalogDir(catalog)))
		if err != nil {
			return false
		}
		if !indexFileStat.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func (s *LocalDirV1) catalogDir(catalog string) string {
	return filepath.Join(s.RootDir, catalog)
}

func catalogFilePath(catalogDir string) string {
	return filepath.Join(catalogDir, "catalog.jsonl")
}

func catalogIndexFilePath(catalogDir string) string {
	return filepath.Join(catalogDir, "index.json")
}

type storeMetasFunc func(catalogDir string, metaChan <-chan *declcfg.Meta) error

func storeCatalogData(catalogDir string, metas <-chan *declcfg.Meta) error {
	f, err := os.Create(catalogFilePath(catalogDir))
	if err != nil {
		return err
	}
	defer f.Close()

	for m := range metas {
		if _, err := f.Write(m.Blob); err != nil {
			return err
		}
	}
	return nil
}

func storeIndexData(catalogDir string, metas <-chan *declcfg.Meta) error {
	idx := newIndex(metas)

	f, err := os.Create(catalogIndexFilePath(catalogDir))
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return enc.Encode(idx)
}

func (s *LocalDirV1) BaseURL(catalog string) string {
	return s.RootURL.JoinPath(catalog).String()
}

// StorageServerHandler returns an HTTP handler for serving catalog content
// This implements the Instance interface for backward compatibility
func (s *LocalDirV1) StorageServerHandler() http.Handler {
	handlers := server.NewCatalogHandlers(s, s.graphqlSvc, s.RootURL, s.EnableMetasHandler)
	return handlers.Handler()
}

// GetCatalogData returns the catalog file and its metadata
// Implements server.CatalogStore interface
func (s *LocalDirV1) GetCatalogData(catalog string) (*os.File, os.FileInfo, error) {
	s.m.RLock()
	defer s.m.RUnlock()

	catalogFile, err := os.Open(catalogFilePath(s.catalogDir(catalog)))
	if err != nil {
		return nil, nil, err
	}
	catalogFileStat, err := catalogFile.Stat()
	if err != nil {
		catalogFile.Close()
		return nil, nil, err
	}
	return catalogFile, catalogFileStat, nil
}

// GetCatalogFS returns a filesystem interface for the catalog
// Implements server.CatalogStore interface
func (s *LocalDirV1) GetCatalogFS(catalog string) (fs.FS, error) {
	s.m.RLock()
	defer s.m.RUnlock()

	catalogDir := s.catalogDir(catalog)
	return os.DirFS(catalogDir), nil
}

// GetIndex returns the index for a catalog
// Implements server.CatalogStore interface
func (s *LocalDirV1) GetIndex(catalog string) (server.Index, error) {
	s.m.RLock()
	defer s.m.RUnlock()

	idx, err, _ := s.sf.Do(catalog, func() (interface{}, error) {
		indexFile, err := os.Open(catalogIndexFilePath(s.catalogDir(catalog)))
		if err != nil {
			return nil, err
		}
		defer indexFile.Close()
		var idx index
		if err := json.NewDecoder(indexFile).Decode(&idx); err != nil {
			return nil, err
		}
		return &idx, nil
	})
	if err != nil {
		return nil, err
	}
	return idx.(*index), nil
}
