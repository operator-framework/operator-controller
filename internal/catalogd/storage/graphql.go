package storage

import (
	"context"
	"iter"
	"os"
	"sync"

	"github.com/graphql-go/graphql"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/catalogd/storage/index"
	catalogdgraphql "github.com/operator-framework/operator-controller/internal/catalogd/storage/internal/graphql"
)

var _ Instance = (*graphQLSchemas)(nil)

type graphQLSchemas struct {
	schemas map[string]*graphql.Schema
	mu      sync.RWMutex
}

type GraphQLSchemas interface {
	Get(catalogName string) (*graphql.Schema, error)
}

func newGraphQLSchemas() *graphQLSchemas {
	return &graphQLSchemas{
		schemas: make(map[string]*graphql.Schema),
	}
}

func (i *graphQLSchemas) Store(ctx context.Context, catalog string, seq iter.Seq2[*declcfg.Meta, error]) error {
	schema, err := catalogdgraphql.GenerateSchema(ctx, seq)
	if err != nil {
		return err
	}

	i.mu.Lock()
	defer i.mu.Unlock()
	i.schemas[catalog] = schema
	return nil
}

func (i *graphQLSchemas) Delete(_ context.Context, catalog string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	delete(i.schemas, catalog)
	return nil
}

func (i *graphQLSchemas) Exists(catalog string) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()

	_, schemaExists := i.schemas[catalog]
	return schemaExists
}

func (i *graphQLSchemas) Get(catalog string) (*graphql.Schema, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	schema, ok := i.schemas[catalog]
	if !ok {
		return nil, os.ErrNotExist
	}
	return schema, nil
}

func ContextWithCatalogData(ctx context.Context, catalogFile *os.File, catalogIndex *index.Index) context.Context {
	return catalogdgraphql.ContextWithCatalogData(ctx, catalogFile, catalogIndex)
}
