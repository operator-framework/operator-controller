package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/graphql-go/graphql"
	gql "github.com/operator-framework/operator-controller/internal/catalogd/graphql"
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
	// this singleflight Group is used in `getIndex()`` to handle concurrent HTTP requests
	// optimally. With the use of this slightflight group, the index is loaded from disk
	// once per concurrent group of HTTP requests being handled by the metas handler.
	// The single flight instance gives us a way to load the index from disk exactly once
	// per concurrent group of callers, and then let every concurrent caller have access to
	// the loaded index. This avoids lots of unnecessary open/decode/close cycles when concurrent
	// requests are being handled, which improves overall performance and decreases response latency.
	sf singleflight.Group
}

var (
	_                Instance = (*LocalDirV1)(nil)
	errInvalidParams          = errors.New("invalid parameters")
)

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
	return errors.Join(
		os.RemoveAll(catalogDir),
		os.Rename(tmpCatalogDir, catalogDir),
	)
}

func (s *LocalDirV1) Delete(catalog string) error {
	s.m.Lock()
	defer s.m.Unlock()

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

func (s *LocalDirV1) StorageServerHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(s.RootURL.JoinPath("{catalog}", "api", "v1", "all").Path, s.handleV1All)
	if s.EnableMetasHandler {
		mux.HandleFunc(s.RootURL.JoinPath("{catalog}", "api", "v1", "metas").Path, s.handleV1Metas)
	}
	mux.HandleFunc(s.RootURL.JoinPath("{catalog}", "api", "v1", "graphql").Path, s.handleV1GraphQL)

	allowedMethodsHandler := func(next http.Handler, allowedMethods ...string) http.Handler {
		allowedMethodSet := sets.New[string](allowedMethods...)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Allow POST requests only for GraphQL endpoint
			if r.URL.Path != "" && r.URL.Path[len(r.URL.Path)-7:] != "graphql" && r.Method == http.MethodPost {
				http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
				return
			}
			if !allowedMethodSet.Has(r.Method) {
				http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	return allowedMethodsHandler(mux, http.MethodGet, http.MethodHead, http.MethodPost)
}

func (s *LocalDirV1) handleV1All(w http.ResponseWriter, r *http.Request) {
	s.m.RLock()
	defer s.m.RUnlock()

	catalog := r.PathValue("catalog")
	catalogFile, catalogStat, err := s.catalogData(catalog)
	if err != nil {
		httpError(w, err)
		return
	}
	w.Header().Add("Content-Type", "application/jsonl")
	http.ServeContent(w, r, "", catalogStat.ModTime(), catalogFile)
}

func (s *LocalDirV1) handleV1Metas(w http.ResponseWriter, r *http.Request) {
	s.m.RLock()
	defer s.m.RUnlock()

	// Check for unexpected query parameters
	expectedParams := map[string]bool{
		"schema":  true,
		"package": true,
		"name":    true,
	}

	for param := range r.URL.Query() {
		if !expectedParams[param] {
			httpError(w, errInvalidParams)
			return
		}
	}

	catalog := r.PathValue("catalog")
	catalogFile, catalogStat, err := s.catalogData(catalog)
	if err != nil {
		httpError(w, err)
		return
	}
	defer catalogFile.Close()

	w.Header().Set("Last-Modified", catalogStat.ModTime().UTC().Format(timeFormat))
	done := checkPreconditions(w, r, catalogStat.ModTime())
	if done {
		return
	}

	schema := r.URL.Query().Get("schema")
	pkg := r.URL.Query().Get("package")
	name := r.URL.Query().Get("name")

	if schema == "" && pkg == "" && name == "" {
		// If no parameters are provided, return the entire catalog (this is the same as /api/v1/all)
		serveJSONLines(w, r, catalogFile)
		return
	}
	idx, err := s.getIndex(catalog)
	if err != nil {
		httpError(w, err)
		return
	}
	indexReader := idx.Get(catalogFile, schema, pkg, name)
	serveJSONLines(w, r, indexReader)
}

func (s *LocalDirV1) handleV1GraphQL(w http.ResponseWriter, r *http.Request) {
	s.m.RLock()
	defer s.m.RUnlock()

	if r.Method != http.MethodPost {
		http.Error(w, "Only POST is allowed", http.StatusMethodNotAllowed)
		return
	}

	catalog := r.PathValue("catalog")
	catalogFile, _, err := s.catalogData(catalog)
	if err != nil {
		httpError(w, err)
		return
	}
	defer catalogFile.Close()

	// Parse GraphQL query from request body
	var params struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Create catalog filesystem from the stored data
	catalogFS, err := s.createCatalogFS(catalog)
	if err != nil {
		httpError(w, err)
		return
	}

	// Build dynamic GraphQL schema for this catalog
	dynamicSchema, err := s.buildCatalogGraphQLSchema(catalogFS)
	if err != nil {
		httpError(w, err)
		return
	}

	// Execute GraphQL query
	result := graphql.Do(graphql.Params{
		Schema:        dynamicSchema.Schema,
		RequestString: params.Query,
	})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		httpError(w, err)
		return
	}
}

func (s *LocalDirV1) catalogData(catalog string) (*os.File, os.FileInfo, error) {
	catalogFile, err := os.Open(catalogFilePath(s.catalogDir(catalog)))
	if err != nil {
		return nil, nil, err
	}
	catalogFileStat, err := catalogFile.Stat()
	if err != nil {
		return nil, nil, err
	}
	return catalogFile, catalogFileStat, nil
}

func httpError(w http.ResponseWriter, err error) {
	var code int
	switch {
	case errors.Is(err, fs.ErrNotExist):
		code = http.StatusNotFound
	case errors.Is(err, fs.ErrPermission):
		code = http.StatusForbidden
	case errors.Is(err, errInvalidParams):
		code = http.StatusBadRequest
	default:
		code = http.StatusInternalServerError
	}
	http.Error(w, fmt.Sprintf("%d %s", code, http.StatusText(code)), code)
}

func serveJSONLines(w http.ResponseWriter, r *http.Request, rs io.Reader) {
	w.Header().Add("Content-Type", "application/jsonl")
	// Copy the content of the reader to the response writer
	// only if it's a Get request
	if r.Method == http.MethodHead {
		return
	}
	_, err := io.Copy(w, rs)
	if err != nil {
		httpError(w, err)
		return
	}
}

func (s *LocalDirV1) getIndex(catalog string) (*index, error) {
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

// createCatalogFS creates a filesystem interface for the catalog data
func (s *LocalDirV1) createCatalogFS(catalog string) (fs.FS, error) {
	catalogDir := s.catalogDir(catalog)
	return os.DirFS(catalogDir), nil
}

// buildCatalogGraphQLSchema builds a dynamic GraphQL schema for the given catalog
func (s *LocalDirV1) buildCatalogGraphQLSchema(catalogFS fs.FS) (*gql.DynamicSchema, error) {
	var metas []*declcfg.Meta

	// Collect all metas from the catalog filesystem
	err := declcfg.WalkMetasFS(context.Background(), catalogFS, func(path string, meta *declcfg.Meta, err error) error {
		if err != nil {
			return err
		}
		if meta != nil {
			metas = append(metas, meta)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error walking catalog metas: %w", err)
	}

	// Discover schema from collected metas
	catalogSchema, err := gql.DiscoverSchemaFromMetas(metas)
	if err != nil {
		return nil, fmt.Errorf("error discovering schema: %w", err)
	}

	// Organize metas by schema for resolvers
	metasBySchema := make(map[string][]*declcfg.Meta)
	for _, meta := range metas {
		if meta.Schema != "" {
			metasBySchema[meta.Schema] = append(metasBySchema[meta.Schema], meta)
		}
	}

	// Build dynamic GraphQL schema
	dynamicSchema, err := gql.BuildDynamicGraphQLSchema(catalogSchema, metasBySchema)
	if err != nil {
		return nil, fmt.Errorf("error building GraphQL schema: %w", err)
	}

	return dynamicSchema, nil
}
