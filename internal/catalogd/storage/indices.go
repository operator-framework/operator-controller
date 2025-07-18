package storage

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"os"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/catalogd/storage/index"
)

var _ Instance = (*indices)(nil)

type indices struct {
	rootDir string
	indices map[string]string
	mu      sync.RWMutex

	sf singleflight.Group
}

type Indices interface {
	Get(catalogName string) (*index.Index, error)
}

func newIndices(rootDir string) *indices {
	return &indices{
		rootDir: rootDir,
		indices: make(map[string]string),
	}
}

func (i *indices) Store(ctx context.Context, catalog string, seq iter.Seq2[*declcfg.Meta, error]) error {
	indexFile, err := os.CreateTemp(i.rootDir, fmt.Sprintf("catalog-index-%s-*.json", catalog))
	if err != nil {
		return err
	}
	indexFilePath := indexFile.Name()
	defer func() {
		_ = indexFile.Close()
	}()

	if err := func() error {
		idx, err := index.New(ctx, seq)
		if err != nil {
			return err
		}
		if _, err := idx.WriteTo(indexFile); err != nil {
			return err
		}

		i.mu.Lock()
		defer i.mu.Unlock()

		// If there is an existing file, delete it
		existing, preExists := i.indices[catalog]
		if preExists {
			if err := os.Remove(existing); err != nil {
				return err
			}
		}
		i.indices[catalog] = indexFilePath
		return nil
	}(); err != nil {
		return errors.Join(err, os.Remove(indexFilePath))
	}
	return nil
}

func (i *indices) Delete(_ context.Context, catalog string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	indexFilePath, ok := i.indices[catalog]
	if !ok {
		return nil
	}
	if err := os.Remove(indexFilePath); err != nil {
		return err
	}
	delete(i.indices, catalog)
	return nil
}

func (i *indices) Exists(catalog string) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()

	indexFilePath, isCatalogRegistered := i.indices[catalog]
	if !isCatalogRegistered {
		return false
	}
	indexFileStat, err := os.Stat(indexFilePath)
	if err != nil {
		return false
	}
	return !indexFileStat.IsDir()
}

func (i *indices) Get(catalog string) (*index.Index, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	indexFilePath, ok := i.indices[catalog]
	if !ok {
		return nil, os.ErrNotExist
	}
	return i.readIndexFromFile(indexFilePath)
}

func (i *indices) readIndexFromFile(indexFilePath string) (*index.Index, error) {
	idx, err, _ := i.sf.Do(indexFilePath, func() (interface{}, error) {
		return index.ReadFile(indexFilePath)
	})
	if err != nil {
		return nil, err
	}
	return idx.(*index.Index), nil
}
