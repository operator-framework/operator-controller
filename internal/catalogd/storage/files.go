package storage

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"os"
	"sync"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

var _ Instance = (*files)(nil)

type files struct {
	rootDir string
	files   map[string]string
	mu      sync.RWMutex
}

type Files interface {
	Get(catalogName string) (*os.File, error)
}

func newFiles(rootDir string) *files {
	return &files{
		rootDir: rootDir,
		files:   make(map[string]string),
	}
}

func (f *files) Store(ctx context.Context, catalog string, seq iter.Seq2[*declcfg.Meta, error]) error {
	catalogFile, err := os.CreateTemp(f.rootDir, fmt.Sprintf("catalog-all-%s-*.jsonl", catalog))
	if err != nil {
		return err
	}
	catalogFilePath := catalogFile.Name()
	defer func() {
		_ = catalogFile.Close()
	}()

	if err := func() error {
		for meta, iterErr := range seq {
			if iterErr != nil {
				return iterErr
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			_, err := catalogFile.Write(meta.Blob)
			if err != nil {
				return err
			}
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		// If there is an existing file, delete it
		existing, preExists := f.files[catalog]
		if preExists {
			if err := os.Remove(existing); err != nil {
				return err
			}
		}
		f.files[catalog] = catalogFilePath
		return nil
	}(); err != nil {
		return errors.Join(err, os.Remove(catalogFilePath))
	}
	return nil
}

func (f *files) Delete(_ context.Context, catalog string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	catalogFilePath, ok := f.files[catalog]
	if !ok {
		return nil
	}
	if err := os.Remove(catalogFilePath); err != nil {
		return err
	}
	delete(f.files, catalog)
	return nil
}

func (f *files) Exists(catalog string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	catalogFilePath, isCatalogRegistered := f.files[catalog]
	if !isCatalogRegistered {
		return false
	}
	catalogFileStat, err := os.Stat(catalogFilePath)
	if err != nil {
		return false
	}
	return !catalogFileStat.IsDir()
}

func (f *files) Get(catalog string) (*os.File, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	filePath, ok := f.files[catalog]
	if !ok {
		return nil, os.ErrNotExist
	}
	return os.Open(filePath)
}
