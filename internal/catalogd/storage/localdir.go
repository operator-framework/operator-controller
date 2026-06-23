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

	gql "github.com/operator-framework/operator-controller/internal/catalogd/graphql"
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
		s.graphqlSvc = service.NewCachedGraphQLService(s)
	}
	return s
}

func (s *LocalDirV1) Store(ctx context.Context, catalog string, fsys fs.FS) error {
	s.m.Lock()
	catalogDir, err := s.storeAtomicSwap(ctx, catalog, fsys)
	if err != nil {
		s.m.Unlock()
		return err
	}
	// Invalidate under the write lock so no concurrent query can serve the old
	// cached schema (with stale byte offsets) against the newly-swapped files.
	if s.graphqlSvc != nil {
		s.graphqlSvc.InvalidateCache(catalog)
	}
	s.m.Unlock()

	if s.graphqlSvc != nil {
		if _, err := s.graphqlSvc.GetSchema(ctx, catalog); err != nil {
			s.graphqlSvc.InvalidateCache(catalog)
			s.m.Lock()
			removeErr := os.RemoveAll(catalogDir)
			s.m.Unlock()
			if removeErr != nil {
				return fmt.Errorf("failed to pre-build GraphQL schema for catalog %q: %w (rollback also failed: %v)", catalog, err, removeErr)
			}
			return fmt.Errorf("failed to pre-build GraphQL schema for catalog %q: %w", catalog, err)
		}
	}

	return nil
}

// storeAtomicSwap writes catalog data to a temp dir and atomically swaps it
// into place. Caller must hold s.m write lock.
func (s *LocalDirV1) storeAtomicSwap(ctx context.Context, catalog string, fsys fs.FS) (string, error) {
	if err := os.MkdirAll(s.RootDir, 0700); err != nil {
		return "", err
	}

	if err := s.removeOrphanedTempDirs(catalog); err != nil {
		return "", fmt.Errorf("error removing orphaned temp directories: %w", err)
	}

	tmpCatalogDir, err := os.MkdirTemp(s.RootDir, fmt.Sprintf(".%s-*", catalog))
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpCatalogDir)

	storeMetaFuncs := []storeMetasFunc{storeCatalogData}
	if s.EnableMetasHandler || s.EnableGraphQLQueries == GraphQLQueriesEnabled {
		storeMetaFuncs = append(storeMetaFuncs, storeIndexData)
	}
	if s.EnableGraphQLQueries == GraphQLQueriesEnabled {
		storeMetaFuncs = append(storeMetaFuncs, discoverAndStoreSchema)
	}

	eg, egCtx := errgroup.WithContext(ctx)
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
		return "", fmt.Errorf("error walking FBC root: %w", err)
	}

	if err := eg.Wait(); err != nil {
		return "", err
	}

	catalogDir := s.catalogDir(catalog)
	err = errors.Join(
		os.RemoveAll(catalogDir),
		os.Rename(tmpCatalogDir, catalogDir),
	)
	if err != nil {
		return "", err
	}

	return catalogDir, nil
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
		return false
	}

	if s.EnableMetasHandler || s.EnableGraphQLQueries == GraphQLQueriesEnabled {
		indexFileStat, err := os.Stat(catalogIndexFilePath(s.catalogDir(catalog)))
		if err != nil {
			return false
		}
		if !indexFileStat.Mode().IsRegular() {
			return false
		}
	}
	if s.EnableGraphQLQueries == GraphQLQueriesEnabled {
		schemaFileStat, err := os.Stat(catalogSchemaFilePath(s.catalogDir(catalog)))
		if err != nil {
			return false
		}
		if !schemaFileStat.Mode().IsRegular() {
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

func catalogSchemaFilePath(catalogDir string) string {
	return filepath.Join(catalogDir, "graphql-schema.json")
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

func discoverAndStoreSchema(catalogDir string, metas <-chan *declcfg.Meta) error {
	catalogSchema, err := gql.DiscoverSchemaFromChannel(metas)
	if err != nil {
		return fmt.Errorf("error discovering schema: %w", err)
	}

	data, err := gql.MarshalCatalogSchema(catalogSchema)
	if err != nil {
		return fmt.Errorf("error marshaling catalog schema: %w", err)
	}

	return os.WriteFile(catalogSchemaFilePath(catalogDir), data, 0600)
}

// LoadCatalogSchema loads the pre-built catalog schema metadata from disk.
// Implements service.CatalogDataProvider.
func (s *LocalDirV1) LoadCatalogSchema(catalog string) (*gql.CatalogSchema, error) {
	s.m.RLock()
	defer s.m.RUnlock()

	data, err := os.ReadFile(catalogSchemaFilePath(s.catalogDir(catalog)))
	if err != nil {
		return nil, err
	}
	return gql.UnmarshalCatalogSchema(data)
}

// NewObjectLoader creates an ObjectLoader that reads objects from the catalog's
// JSONL file using index-based byte offsets. Each query reads only the requested
// page from disk instead of holding all parsed objects in memory.
// Implements service.CatalogDataProvider.
func (s *LocalDirV1) NewObjectLoader(catalog string) (gql.ObjectLoader, error) {
	idx, err := s.loadIndex(catalog)
	if err != nil {
		return nil, fmt.Errorf("error loading index for catalog %q: %w", catalog, err)
	}

	catalogPath := catalogFilePath(s.catalogDir(catalog))

	return func(schemaName string, offset, limit int) ([]map[string]interface{}, error) {
		s.m.RLock()
		defer s.m.RUnlock()

		sections := idx.GetSchemaSections(schemaName)
		if sections == nil {
			return nil, nil
		}

		if offset >= len(sections) {
			return nil, nil
		}
		sections = sections[offset:]

		if limit < len(sections) {
			sections = sections[:limit]
		}

		f, err := os.Open(catalogPath)
		if err != nil {
			return nil, fmt.Errorf("error opening catalog file: %w", err)
		}
		defer f.Close()

		const maxEntrySize = 16 * 1024 * 1024 // 16 MiB
		results := make([]map[string]interface{}, 0, len(sections))
		for _, sec := range sections {
			if sec.Length <= 0 || sec.Length > maxEntrySize {
				return nil, fmt.Errorf("invalid section length %d at offset %d", sec.Length, sec.Offset)
			}
			buf := make([]byte, sec.Length)
			if _, err := f.ReadAt(buf, sec.Offset); err != nil {
				return nil, fmt.Errorf("error reading section at offset %d: %w", sec.Offset, err)
			}

			var obj map[string]interface{}
			if err := json.Unmarshal(buf, &obj); err != nil {
				continue
			}
			results = append(results, obj)
		}

		return results, nil
	}, nil
}

// loadIndex loads the index from disk using singleflight for efficiency.
func (s *LocalDirV1) loadIndex(catalog string) (*index, error) {
	s.m.RLock()
	defer s.m.RUnlock()

	result, err, _ := s.sf.Do(catalog, func() (interface{}, error) {
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
	return result.(*index), nil
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
		if errors.Is(err, fs.ErrNotExist) {
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
	return s.loadIndex(catalog)
}
