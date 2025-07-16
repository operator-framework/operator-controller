package graphql

import (
	"errors"
	"io"
	"maps"
	"os"
	"slices"
	"sync"

	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

type GraphQLResolver struct {
	indexBySchema map[string]graphQLResolverSchemaIndex
	data          *os.File
	writePosition int64
	mu            sync.RWMutex
}

type graphQLResolverSchemaIndex struct {
	byPackage        map[string][]*index
	byName           map[string][]*index
	byPackageAndName map[[2]string][]*index
}

type index struct {
	start  int64
	length int64
}

func NewGraphQLResolver(tmpDir string) (*GraphQLResolver, error) {
	dataFile, err := os.CreateTemp(tmpDir, "graphql-resolver-*.data")
	if err != nil {
		return nil, err
	}

	return &GraphQLResolver{
		indexBySchema: make(map[string]graphQLResolverSchemaIndex),
		data:          dataFile,
		writePosition: 0,
	}, nil
}

func (r *GraphQLResolver) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return errors.Join(r.data.Close(), os.Remove(r.data.Name()))
}

func (r *GraphQLResolver) IndexMeta(meta *declcfg.Meta) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	start := r.writePosition
	length := int64(len(meta.Blob))

	if _, err := r.data.Write(meta.Blob); err != nil {
		return err
	}

	idx := &index{
		start:  start,
		length: length,
	}

	schemaIndex, ok := r.indexBySchema[meta.Schema]
	if !ok {
		schemaIndex = graphQLResolverSchemaIndex{
			byPackage:        make(map[string][]*index),
			byName:           make(map[string][]*index),
			byPackageAndName: make(map[[2]string][]*index),
		}
	}

	schemaIndex.byPackage[meta.Package] = append(schemaIndex.byPackage[meta.Package], idx)
	schemaIndex.byName[meta.Name] = append(schemaIndex.byName[meta.Name], idx)
	schemaIndex.byPackageAndName[[2]string{meta.Package, meta.Name}] = append(schemaIndex.byPackageAndName[[2]string{meta.Package, meta.Name}], idx)

	r.indexBySchema[meta.Schema] = schemaIndex
	r.writePosition += length

	return nil
}

func (r *GraphQLResolver) GetBySchema(schema string) io.Reader {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemaIndex, ok := r.indexBySchema[schema]
	if !ok {
		return io.MultiReader()
	}

	multiReaders := make([]io.Reader, 0, len(schemaIndex.byPackage))
	for _, pkgName := range slices.Sorted(maps.Keys(schemaIndex.byPackage)) {
		multiReaders = append(multiReaders, r.multiReaderFor(schemaIndex.byPackage[pkgName]))
	}
	return io.MultiReader(multiReaders...)
}

func (r *GraphQLResolver) GetByPackage(schema, packageName string) io.Reader {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemaIndex, ok := r.indexBySchema[schema]
	if !ok {
		return io.MultiReader()
	}

	indexes := schemaIndex.byPackage[packageName]
	return r.multiReaderFor(indexes)
}

func (r *GraphQLResolver) GetByName(schema, name string) io.Reader {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemaIndex, ok := r.indexBySchema[schema]
	if !ok {
		return io.MultiReader()
	}

	indexes := schemaIndex.byName[name]
	return r.multiReaderFor(indexes)
}

func (r *GraphQLResolver) GetByPackageAndName(schema, packageName, name string) io.Reader {
	r.mu.RLock()
	defer r.mu.RUnlock()

	schemaIndex, ok := r.indexBySchema[schema]
	if !ok {
		return io.MultiReader()
	}
	return r.multiReaderFor(schemaIndex.byPackageAndName[[2]string{packageName, name}])
}

func (r *GraphQLResolver) multiReaderFor(indexes []*index) io.Reader {
	sectionReaders := make([]io.Reader, 0, len(indexes))
	for _, idx := range indexes {
		sectionReaders = append(sectionReaders, io.NewSectionReader(r.data, idx.start, idx.length))
	}
	return io.MultiReader(sectionReaders...)
}
