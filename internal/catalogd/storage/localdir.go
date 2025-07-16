package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

// LocalDirV1 is a storage Instance. When Storing a new FBC contained in
// fs.FS, the content is first written to a temporary file, after which
// it is copied to its final destination in RootDir/<catalogName>.jsonl. This is
// done so that clients accessing the content stored in RootDir/<catalogName>.json1
// have an atomic view of the content for a catalog.
type LocalDirV1 struct {
	RootDir  string
	RootURL  *url.URL
	Indexer  *Indexer
	Handlers map[string]http.Handler
	m        sync.RWMutex
}

var (
	_ Instance = (*LocalDirV1)(nil)
)

type MetaProcessor interface {
	ProcessMetas(ctx context.Context, catalog string, idx *Index, metasChan <-chan *declcfg.Meta) error
}

func (s *LocalDirV1) Store(ctx context.Context, catalog string, fsys fs.FS) error {
	s.m.Lock()
	defer s.m.Unlock()

	catalogDir := filepath.Join(s.RootDir, catalog)
	if err := os.MkdirAll(catalogDir, 0700); err != nil {
		return err
	}

	idx, err := InitIndex(catalogDir, true)
	if err != nil {
		return err
	}

	var metaProcessors []MetaProcessor
	for _, handler := range s.Handlers {
		if handler, ok := handler.(MetaProcessor); ok {
			metaProcessors = append(metaProcessors, handler)
		}
	}
	eg, egCtx := errgroup.WithContext(ctx)
	metasChans := make([]chan *declcfg.Meta, 0, len(metaProcessors))
	for range metaProcessors {
		metasChans = append(metasChans, make(chan *declcfg.Meta, 1))
	}
	for i, mp := range metaProcessors {
		eg.Go(func() error {
			return mp.ProcessMetas(egCtx, catalog, idx, metasChans[i])
		})
	}

	if err := func() error {
		defer func() {
			for i := range metasChans {
				close(metasChans[i])
			}
		}()
		return declcfg.WalkMetasFS(ctx, fsys, func(path string, meta *declcfg.Meta, err error) error {
			if err != nil {
				return err
			}
			if err := idx.ProcessMeta(meta); err != nil {
				return fmt.Errorf("failed to process meta: %w", err)
			}

			for _, ch := range metasChans {
				select {
				case ch <- meta:
				case <-egCtx.Done():
					return nil
				}
			}

			return nil
		}, declcfg.WithConcurrency(1))
	}(); err != nil {
		idx.Close()
		return fmt.Errorf("error walking FBC root: %w", err)
	}

	if err := eg.Wait(); err != nil {
		idx.Close()
		return fmt.Errorf("error processing metas: %w", err)
	}

	if err := s.Indexer.CommitIndex(catalog, idx); err != nil {
		idx.Close()
		return err
	}
	return nil
}

func (s *LocalDirV1) Delete(catalog string) error {
	s.m.Lock()
	defer s.m.Unlock()

	return errors.Join(s.Indexer.DeleteIndex(catalog), os.RemoveAll(filepath.Join(s.RootDir, catalog)))
}

func (s *LocalDirV1) ContentExists(catalog string) bool {
	s.m.RLock()
	defer s.m.RUnlock()

	idx, err := s.Indexer.GetIndex(catalog)
	if err != nil {
		return false
	}
	return idx.ContentExists()
}

func (s *LocalDirV1) BaseURL(catalog string) string {
	return s.RootURL.JoinPath(catalog).String()
}

func (s *LocalDirV1) StorageServerHandler() http.Handler {
	mux := http.NewServeMux()
	for path, handler := range s.Handlers {
		mux.Handle(s.RootURL.JoinPath("{catalog}", "api", "v1", path).Path, handler)
	}
	return mux
}
