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
	"strings"
	"sync"

	"github.com/klauspost/compress/gzhttp"
	"golang.org/x/sync/errgroup"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/catalogd/internal/features"
)

// LocalDirV1 is a storage Instance. When Storing a new FBC contained in
// fs.FS, the content is first written to a temporary file, after which
// it is copied to its final destination in RootDir/catalogName/. This is
// done so that clients accessing the content stored in RootDir/catalogName have
// atomic view of the content for a catalog.
type LocalDirV1 struct {
	RootDir string
	RootURL *url.URL

	m sync.RWMutex
}

func (s *LocalDirV1) Store(ctx context.Context, catalog string, fsys fs.FS) error {
	s.m.Lock()
	defer s.m.Unlock()

	if features.CatalogdFeatureGate.Enabled(features.APIV1QueryHandler) {
		return s.storeCatalogFileAndIndex(ctx, catalog, fsys)
	}
	return s.storeCatalogFile(ctx, catalog, fsys)
}

func (s *LocalDirV1) storeCatalogFile(ctx context.Context, catalog string, fsys fs.FS) error {
	if err := os.MkdirAll(s.RootDir, 0700); err != nil {
		return err
	}
	tmpCatalogFile, err := os.CreateTemp(s.RootDir, fmt.Sprintf(".%s-*.jsonl", catalog))
	if err != nil {
		return err
	}
	defer os.Remove(tmpCatalogFile.Name())

	if err := declcfg.WalkMetasFS(ctx, fsys, func(path string, meta *declcfg.Meta, err error) error {
		if err != nil {
			return err
		}
		_, err = tmpCatalogFile.Write(meta.Blob)
		return err
	}); err != nil {
		return fmt.Errorf("error walking FBC root: %w", err)
	}

	fbcFile := filepath.Join(s.RootDir, fmt.Sprintf("%s.jsonl", catalog))
	return os.Rename(tmpCatalogFile.Name(), fbcFile)
}

func (s *LocalDirV1) storeCatalogFileAndIndex(ctx context.Context, catalog string, fsys fs.FS) error {
	if err := os.MkdirAll(s.RootDir, 0700); err != nil {
		return err
	}
	tmpCatalogFile, err := os.CreateTemp(s.RootDir, fmt.Sprintf(".%s-*.jsonl", catalog))
	if err != nil {
		return err
	}
	defer os.Remove(tmpCatalogFile.Name())

	tmpIndexFile, err := os.CreateTemp(s.RootDir, filepath.Base(fmt.Sprintf("%s.index.json", strings.TrimSuffix(tmpCatalogFile.Name(), ".jsonl"))))
	if err != nil {
		return err
	}
	defer os.Remove(tmpIndexFile.Name())

	pr, pw := io.Pipe()
	mw := io.MultiWriter(tmpCatalogFile, pw)
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		if err := declcfg.WalkMetasFS(egCtx, fsys, func(path string, meta *declcfg.Meta, err error) error {
			if err != nil {
				return err
			}
			_, err = mw.Write(meta.Blob)
			if err != nil {
				return pw.CloseWithError(err)
			}
			return nil
		}, declcfg.WithConcurrency(1)); err != nil {
			return fmt.Errorf("error walking FBC root: %w", err)
		}
		return pw.CloseWithError(tmpCatalogFile.Close())
	})
	eg.Go(func() error {
		idx, err := newIndex(pr)
		if err != nil {
			return pr.CloseWithError(err)
		}
		if err := pr.Close(); err != nil {
			return err
		}
		enc := json.NewEncoder(tmpIndexFile)
		if err := enc.Encode(idx); err != nil {
			return err
		}
		if err := tmpIndexFile.Close(); err != nil {
			return err
		}
		return nil
	})
	if err := eg.Wait(); err != nil {
		return err
	}

	fbcFile := filepath.Join(s.RootDir, fmt.Sprintf("%s.jsonl", catalog))
	fbcIndexFile := filepath.Join(s.RootDir, fmt.Sprintf("%s.index.json", catalog))
	return errors.Join(
		os.Rename(tmpCatalogFile.Name(), fbcFile),
		os.Rename(tmpIndexFile.Name(), fbcIndexFile),
	)
}

func (s *LocalDirV1) Delete(catalog string) error {
	s.m.Lock()
	defer s.m.Unlock()

	var errs []error
	errs = append(errs, os.RemoveAll(filepath.Join(s.RootDir, fmt.Sprintf("%s.jsonl", catalog))))

	if features.CatalogdFeatureGate.Enabled(features.APIV1QueryHandler) {
		errs = append(errs, os.RemoveAll(filepath.Join(s.RootDir, fmt.Sprintf("%s.index.json", catalog))))
	}
	return errors.Join(errs...)
}

func (s *LocalDirV1) BaseURL(catalog string) string {
	return s.RootURL.JoinPath(catalog).String()
}

func (s *LocalDirV1) StorageServerHandler() http.Handler {
	mux := http.NewServeMux()

	v1AllPath := s.RootURL.JoinPath("{catalog}", "api", "v1", "all").Path
	mux.Handle(v1AllPath, s.v1AllHandler())

	if features.CatalogdFeatureGate.Enabled(features.APIV1QueryHandler) {
		v1QueryPath := s.RootURL.JoinPath("{catalog}", "api", "v1", "query").Path
		mux.Handle(v1QueryPath, s.v1QueryHandler())
	}
	return mux
}

func (s *LocalDirV1) v1AllHandler() http.Handler {
	catalogHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.m.RLock()
		defer s.m.RUnlock()

		catalog := r.PathValue("catalog")
		http.ServeFile(w, r, filepath.Join(s.RootDir, fmt.Sprintf("%s.jsonl", catalog)))
	})
	gzHandler := gzhttp.GzipHandler(catalogHandler)

	typeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/jsonl")
		gzHandler.ServeHTTP(w, r)
	})
	return typeHandler
}

func (s *LocalDirV1) v1QueryHandler() http.Handler {
	catalogHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.m.RLock()
		defer s.m.RUnlock()

		catalog := r.PathValue("catalog")
		schema := r.URL.Query().Get("schema")
		pkg := r.URL.Query().Get("package")
		name := r.URL.Query().Get("name")

		catalogFilePath := filepath.Join(s.RootDir, fmt.Sprintf("%s.jsonl", catalog))
		catalogFileStat, err := os.Stat(catalogFilePath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "Catalog not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		modTime := catalogFileStat.ModTime().Format(http.TimeFormat)
		if r.Header.Get("If-Modified-Since") == modTime {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		catalogFile, err := os.Open(catalogFilePath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "Catalog not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer catalogFile.Close()

		indexFile, err := os.Open(filepath.Join(s.RootDir, fmt.Sprintf("%s.index.json", catalog)))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "No catalog contents found matching query", http.StatusNotFound)
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer indexFile.Close()

		var idx index
		if err := json.NewDecoder(indexFile).Decode(&idx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		queryReader, ok := idx.Get(catalogFile, schema, pkg, name)
		if !ok {
			http.Error(w, fmt.Sprintf("No index found for schema=%q, package=%q, name=%q", schema, pkg, name), http.StatusInternalServerError)
			return
		}
		w.Header().Add("Content-Type", "application/jsonl")
		w.Header().Set("Last-Modified", modTime)
		_, _ = io.Copy(w, queryReader)
	})
	gzHandler := gzhttp.GzipHandler(catalogHandler)
	return gzHandler
}

func (s *LocalDirV1) ContentExists(catalog string) bool {
	s.m.RLock()
	defer s.m.RUnlock()

	catalogFileStat, err := os.Stat(filepath.Join(s.RootDir, fmt.Sprintf("%s.jsonl", catalog)))
	if err != nil {
		return false
	}
	if !catalogFileStat.Mode().IsRegular() {
		// path is not valid content
		return false
	}

	if features.CatalogdFeatureGate.Enabled(features.APIV1QueryHandler) {
		indexFileStat, err := os.Stat(filepath.Join(s.RootDir, fmt.Sprintf("%s.index.json", catalog)))
		if err != nil {
			return false
		}
		if !indexFileStat.Mode().IsRegular() {
			return false
		}
	}
	return true
}
