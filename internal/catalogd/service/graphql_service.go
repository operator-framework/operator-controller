package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
	"golang.org/x/sync/singleflight"

	gql "github.com/operator-framework/operator-controller/internal/catalogd/graphql"
)

// CatalogDataProvider provides access to catalog data for GraphQL schema building.
// Implemented by the storage layer.
type CatalogDataProvider interface {
	LoadCatalogSchema(catalog string) (*gql.CatalogSchema, error)
	NewObjectLoader(catalog string) (gql.ObjectLoader, error)
}

// GraphQLService handles GraphQL schema generation and query execution for catalogs
type GraphQLService interface {
	GetSchema(ctx context.Context, catalog string) (*gql.DynamicSchema, error)
	ExecuteQuery(ctx context.Context, catalog string, query string) (*graphql.Result, error)
	InvalidateCache(catalog string)
}

// CachedGraphQLService implements GraphQLService with an in-memory schema cache.
// The cached DynamicSchema contains only the GraphQL type system and schema metadata
// (a few KB). Object data is loaded from disk at query time via the ObjectLoader.
type CachedGraphQLService struct {
	provider    CatalogDataProvider
	schemaMux   sync.RWMutex
	schemaCache map[string]*gql.DynamicSchema
	buildGroup  singleflight.Group
}

func NewCachedGraphQLService(provider CatalogDataProvider) *CachedGraphQLService {
	return &CachedGraphQLService{
		provider:    provider,
		schemaCache: make(map[string]*gql.DynamicSchema),
	}
}

func (s *CachedGraphQLService) GetSchema(ctx context.Context, catalog string) (*gql.DynamicSchema, error) {
	s.schemaMux.RLock()
	if cachedSchema, ok := s.schemaCache[catalog]; ok {
		s.schemaMux.RUnlock()
		return cachedSchema, nil
	}
	s.schemaMux.RUnlock()

	result, err, _ := s.buildGroup.Do(catalog, func() (interface{}, error) {
		s.schemaMux.RLock()
		if cachedSchema, ok := s.schemaCache[catalog]; ok {
			s.schemaMux.RUnlock()
			return cachedSchema, nil
		}
		s.schemaMux.RUnlock()

		dynamicSchema, err := s.buildSchema(catalog)
		if err != nil {
			return nil, err
		}

		s.schemaMux.Lock()
		s.schemaCache[catalog] = dynamicSchema
		s.schemaMux.Unlock()

		return dynamicSchema, nil
	})

	if err != nil {
		return nil, err
	}

	return result.(*gql.DynamicSchema), nil
}

func (s *CachedGraphQLService) ExecuteQuery(ctx context.Context, catalog string, query string) (*graphql.Result, error) {
	if err := gql.ValidateQueryComplexity(query); err != nil {
		return &graphql.Result{
			Errors: []gqlerrors.FormattedError{
				gqlerrors.FormatError(err),
			},
		}, nil
	}

	dynamicSchema, err := s.GetSchema(ctx, catalog)
	if err != nil {
		return nil, fmt.Errorf("failed to get GraphQL schema: %w", err)
	}

	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	result := graphql.Do(graphql.Params{
		Schema:        dynamicSchema.Schema,
		RequestString: query,
		Context:       queryCtx,
	})

	return result, nil
}

func (s *CachedGraphQLService) InvalidateCache(catalog string) {
	s.schemaMux.Lock()
	delete(s.schemaCache, catalog)
	s.schemaMux.Unlock()
}

func (s *CachedGraphQLService) buildSchema(catalog string) (*gql.DynamicSchema, error) {
	catalogSchema, err := s.provider.LoadCatalogSchema(catalog)
	if err != nil {
		return nil, fmt.Errorf("error loading catalog schema: %w", err)
	}

	loader, err := s.provider.NewObjectLoader(catalog)
	if err != nil {
		return nil, fmt.Errorf("error creating object loader: %w", err)
	}

	dynamicSchema, err := gql.BuildDynamicGraphQLSchema(catalogSchema, loader)
	if err != nil {
		return nil, fmt.Errorf("error building GraphQL schema: %w", err)
	}

	return dynamicSchema, nil
}
