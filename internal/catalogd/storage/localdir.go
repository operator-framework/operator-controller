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
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
	"k8s.io/klog/v2"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/catalogd/server"
	"github.com/operator-framework/operator-controller/internal/catalogd/service"
)

// Re-export enum types and constants from server package for convenience
type (
	MetasHandlerMode   = server.MetasHandlerMode
	GraphQLQueriesMode = server.GraphQLQueriesMode
)

const (
	MetasHandlerDisabled   = server.MetasHandlerDisabled
	MetasHandlerEnabled    = server.MetasHandlerEnabled
	GraphQLQueriesDisabled = server.GraphQLQueriesDisabled
	GraphQLQueriesEnabled  = server.GraphQLQueriesEnabled
)

// LocalDirV1 is a storage Instance. When Storing a new FBC contained in
// fs.FS, the content is first written to a temporary file, after which
// it is copied to its final destination in RootDir/<catalogName>.jsonl. This is
// done so that clients accessing the content stored in RootDir/<catalogName>.json1
// have an atomic view of the content for a catalog.
type LocalDirV1 struct {
	RootDir              string
	RootURL              *url.URL
	EnableMetasHandler   MetasHandlerMode
	EnableGraphQLQueries GraphQLQueriesMode

	m sync.RWMutex
	// this singleflight Group is used in `GetIndex()` to handle concurrent HTTP requests
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
func NewLocalDirV1(rootDir string, rootURL *url.URL, enableMetasHandler MetasHandlerMode, enableGraphQLQueries GraphQLQueriesMode) *LocalDirV1 {
	s := &LocalDirV1{
		RootDir:              rootDir,
		RootURL:              rootURL,
		EnableMetasHandler:   enableMetasHandler,
		EnableGraphQLQueries: enableGraphQLQueries,
	}
	if enableGraphQLQueries == GraphQLQueriesEnabled {
		s.graphqlSvc = service.NewCachedGraphQLService()
	}
	return s
}

func (s *LocalDirV1) Store(ctx context.Context, catalog string, fsys fs.FS) error {
	s.m.Lock()
	defer s.m.Unlock()

	if err := os.MkdirAll(s.RootDir, 0700); err != nil {
		return err
	}

	// Remove any orphaned temporary directories left by previously interrupted Store
	// operations (e.g. after a process crash where deferred cleanup did not run).
	if err := s.removeOrphanedTempDirs(catalog); err != nil {
		return fmt.Errorf("error removing orphaned temp directories: %w", err)
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

	// Invalidate and pre-warm GraphQL schema cache if GraphQL service is enabled
	if s.graphqlSvc != nil {
		s.graphqlSvc.InvalidateCache(catalog)

		// Pre-warm the GraphQL schema cache using the newly created catalog directory
		// Use the actual catalog directory filesystem, not the input fsys
		catalogFS := os.DirFS(catalogDir)
		if _, err := s.graphqlSvc.GetSchema(catalog, catalogFS); err != nil {
			// Schema build failed - rollback by removing the catalog directory
			// to maintain consistency (don't persist catalog without valid schema)
			if removeErr := os.RemoveAll(catalogDir); removeErr != nil {
				return fmt.Errorf("failed to pre-build GraphQL schema for catalog %q: %w (rollback also failed: %v)", catalog, err, removeErr)
			}
			return fmt.Errorf("failed to pre-build GraphQL schema for catalog %q: %w", catalog, err)
		}
	}

	return nil
}

// removeOrphanedTempDirs removes temporary staging directories that were created by a
// previous Store call for the given catalog but were not cleaned up because the process
// was interrupted (e.g. killed by the OOM killer) before the deferred RemoveAll could run.
// Temp dirs use the prefix ".{catalog}-" as created by os.MkdirTemp.
// This method must be called while the write lock is held.
func (s *LocalDirV1) removeOrphanedTempDirs(catalog string) error {
	entries, err := os.ReadDir(s.RootDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("error reading storage directory: %w", err)
	}
	prefix := fmt.Sprintf(".%s-", catalog)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			if err := os.RemoveAll(filepath.Join(s.RootDir, entry.Name())); err != nil {
				return fmt.Errorf("error removing orphaned temp directory %q: %w", entry.Name(), err)
			}
		}
	}
	return nil
}

func (s *LocalDirV1) Delete(catalog string) error {
	s.m.Lock()
	defer s.m.Unlock()

	// Invalidate GraphQL cache if service is enabled
	if s.graphqlSvc != nil {
		s.graphqlSvc.InvalidateCache(catalog)
	}

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
	handlers := server.NewCatalogHandlers(s, s.graphqlSvc, s.RootURL, s.EnableMetasHandler, s.EnableGraphQLQueries)
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
		if closeErr := catalogFile.Close(); closeErr != nil {
			klog.ErrorS(closeErr, "failed to close catalog file after stat error")
		}
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
	info, err := os.Stat(catalogDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("catalog path %q is not a directory", catalogDir)
	}
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
