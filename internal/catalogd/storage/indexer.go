package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

type Indexer struct {
	indices map[string]*Index // catalogName -> index
	mu      sync.RWMutex
}

func NewIndexer() *Indexer {
	return &Indexer{
		indices: make(map[string]*Index),
	}
}

func (i *Indexer) GetIndex(catalogName string) (*Index, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	idx, ok := i.indices[catalogName]
	if !ok {
		return nil, fmt.Errorf("index not found for catalog %s", catalogName)
	}
	return idx, nil
}

func (i *Indexer) CommitIndex(catalogName string, idx *Index) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	existingIndex, ok := i.indices[catalogName]
	if ok {
		if err := existingIndex.Close(); err != nil {
			return err
		}
	}
	i.indices[catalogName] = idx
	return nil
}

func (i *Indexer) DeleteIndex(catalogName string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	index, ok := i.indices[catalogName]
	if !ok {
		return nil
	}
	if err := index.Close(); err != nil {
		return err
	}
	delete(i.indices, catalogName)
	return nil
}

type Index struct {
	indexMetas bool

	dataFile      *os.File
	writePosition int64
	index         *index
	mu            sync.RWMutex
}

func InitIndex(catalogDir string, indexMetas bool) (*Index, error) {
	dataFile, err := os.CreateTemp(catalogDir, "catalog-*.jsonl")
	if err != nil {
		return nil, err
	}
	dataFile.Name()

	return &Index{
		indexMetas: indexMetas,
		dataFile:   dataFile,
		index: &index{
			BySchema:  make(map[string][]section),
			ByPackage: make(map[string][]section),
			ByName:    make(map[string][]section),
		},
	}, nil
}

func (i *Index) ProcessMeta(meta *declcfg.Meta) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	start := i.writePosition
	length := int64(len(meta.Blob))
	i.writePosition += length

	if _, err := i.dataFile.Write(meta.Blob); err != nil {
		return err
	}

	if i.indexMetas {
		s := section{offset: start, length: length}
		if meta.Schema != "" {
			i.index.BySchema[meta.Schema] = append(i.index.BySchema[meta.Schema], s)
		}
		if meta.Package != "" {
			i.index.ByPackage[meta.Package] = append(i.index.ByPackage[meta.Package], s)
		}
		if meta.Name != "" {
			i.index.ByName[meta.Name] = append(i.index.ByName[meta.Name], s)
		}
	}
	return nil
}

func (i *Index) All() *io.SectionReader {
	i.mu.RLock()
	defer i.mu.RUnlock()

	return i.dataReaderNoLock()
}

func (i *Index) Stat() (os.FileInfo, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	return i.dataFile.Stat()
}

func (i *Index) Lookup(schema, pkg, name string) (io.Reader, error) {
	if !i.indexMetas {
		return nil, fmt.Errorf("indexing not enabled")
	}
	i.mu.RLock()
	defer i.mu.RUnlock()

	return i.index.Get(i.dataReaderNoLock(), schema, pkg, name), nil
}

func (i *Index) dataReaderNoLock() *io.SectionReader {
	return io.NewSectionReader(i.dataFile, 0, i.writePosition)
}

func (i *Index) ContentExists() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return true
}

func (i *Index) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	return errors.Join(
		i.dataFile.Close(),
		os.RemoveAll(i.dataFile.Name()),
	)
}
