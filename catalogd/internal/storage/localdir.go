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
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

// LocalDirV1 is a storage Instance. When Storing a new FBC contained in
// fs.FS, the content is first written to a temporary file, after which
// it is copied to its final destination in RootDir/catalogName/. This is
// done so that clients accessing the content stored in RootDir/catalogName have
// atomic view of the content for a catalog.
type LocalDirV1 struct {
	RootDir            string
	RootURL            *url.URL
	EnableQueryHandler bool

	m  sync.RWMutex
	sf singleflight.Group
}

var (
	_                Instance = &LocalDirV1{}
	ErrInvalidParams          = errors.New("invalid parameters")
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
	if s.EnableQueryHandler {
		storeMetaFuncs = append(storeMetaFuncs, storeIndexData)
	}

	var (
		eg, egCtx = errgroup.WithContext(ctx)
		metaChans []chan *declcfg.Meta
	)
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

	if s.EnableQueryHandler {
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
	idx, err := newIndex(metas)
	if err != nil {
		return err
	}

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
	if s.EnableQueryHandler {
		mux.HandleFunc(s.RootURL.JoinPath("{catalog}", "api", "v1", "query").Path, s.handleV1Query)
	}
	return mux
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
	serveJsonLines(w, r, catalogStat.ModTime(), catalogFile)
}

func (s *LocalDirV1) handleV1Query(w http.ResponseWriter, r *http.Request) {
	s.m.RLock()
	defer s.m.RUnlock()

	catalog := r.PathValue("catalog")
	catalogFile, catalogStat, err := s.catalogData(catalog)
	if err != nil {
		httpError(w, err)
		return
	}
	defer catalogFile.Close()

	schema := r.URL.Query().Get("schema")
	pkg := r.URL.Query().Get("package")
	name := r.URL.Query().Get("name")

	if schema == "" && pkg == "" && name == "" {
		// If no parameters are provided, return the entire catalog (this is the same as /api/v1/all)
		serveJsonLines(w, r, catalogStat.ModTime(), catalogFile)
		return
	}
	idx, err := s.getIndex(catalog)
	if err != nil {
		httpError(w, err)
		return
	}
	indexReader, ok := idx.Get(catalogFile, schema, pkg, name)
	if !ok {
		httpError(w, fs.ErrNotExist)
		return
	}
	serveJsonLinesQuery(w, indexReader)
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
	case errors.Is(err, ErrInvalidParams):
		code = http.StatusBadRequest
	default:
		code = http.StatusInternalServerError
	}
	http.Error(w, fmt.Sprintf("%d %s", code, http.StatusText(code)), code)
}

func serveJsonLines(w http.ResponseWriter, r *http.Request, modTime time.Time, rs io.ReadSeeker) {
	w.Header().Add("Content-Type", "application/jsonl")
	http.ServeContent(w, r, "", modTime, rs)
}

func serveJsonLinesQuery(w http.ResponseWriter, rs io.Reader) {
	w.Header().Add("Content-Type", "application/jsonl")
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
