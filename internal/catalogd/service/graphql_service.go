package service

import (
	"context"
	"fmt"
	"io/fs"
	"sync"

	"github.com/graphql-go/graphql"
	gql "github.com/operator-framework/operator-controller/internal/catalogd/graphql"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
)

// GraphQLService handles GraphQL schema generation and query execution for catalogs
type GraphQLService interface {
	// GetSchema returns the GraphQL schema for a catalog, using cache if available
	GetSchema(catalog string, catalogFS fs.FS) (*gql.DynamicSchema, error)

	// ExecuteQuery executes a GraphQL query against a catalog
	ExecuteQuery(catalog string, catalogFS fs.FS, query string) (*graphql.Result, error)

	// InvalidateCache removes the cached schema for a catalog
	InvalidateCache(catalog string)
}

// CachedGraphQLService implements GraphQLService with an in-memory schema cache
type CachedGraphQLService struct {
	schemaMux   sync.RWMutex
	schemaCache map[string]*gql.DynamicSchema
}

// NewCachedGraphQLService creates a new GraphQL service with caching
func NewCachedGraphQLService() *CachedGraphQLService {
	return &CachedGraphQLService{
		schemaCache: make(map[string]*gql.DynamicSchema),
	}
}

// GetSchema returns the GraphQL schema for a catalog, using cache if available
func (s *CachedGraphQLService) GetSchema(catalog string, catalogFS fs.FS) (*gql.DynamicSchema, error) {
	// Check cache first (read lock)
	s.schemaMux.RLock()
	if cachedSchema, ok := s.schemaCache[catalog]; ok {
		s.schemaMux.RUnlock()
		return cachedSchema, nil
	}
	s.schemaMux.RUnlock()

	// Schema not in cache, build it
	dynamicSchema, err := buildSchemaFromFS(catalogFS)
	if err != nil {
		return nil, err
	}

	// Cache the result (write lock)
	s.schemaMux.Lock()
	s.schemaCache[catalog] = dynamicSchema
	s.schemaMux.Unlock()

	return dynamicSchema, nil
}

// ExecuteQuery executes a GraphQL query against a catalog
func (s *CachedGraphQLService) ExecuteQuery(catalog string, catalogFS fs.FS, query string) (*graphql.Result, error) {
	// Get or build the schema
	// TODO: prevent cache rebuild on this callpath
	dynamicSchema, err := s.GetSchema(catalog, catalogFS)
	if err != nil {
		return nil, fmt.Errorf("failed to get GraphQL schema: %w", err)
	}

	// Execute the query
	result := graphql.Do(graphql.Params{
		Schema:        dynamicSchema.Schema,
		RequestString: query,
	})

	return result, nil
}

// InvalidateCache removes the cached schema for a catalog
func (s *CachedGraphQLService) InvalidateCache(catalog string) {
	s.schemaMux.Lock()
	delete(s.schemaCache, catalog)
	s.schemaMux.Unlock()
}

// buildSchemaFromFS builds a GraphQL schema from a catalog filesystem
func buildSchemaFromFS(catalogFS fs.FS) (*gql.DynamicSchema, error) {
	var metas []*declcfg.Meta
	var metasMux sync.Mutex

	// Collect all metas from the catalog filesystem
	// WalkMetasFS walks the filesystem concurrently, so we need to protect the metas slice
	err := declcfg.WalkMetasFS(context.Background(), catalogFS, func(path string, meta *declcfg.Meta, err error) error {
		if err != nil {
			return err
		}
		if meta != nil {
			metasMux.Lock()
			metas = append(metas, meta)
			metasMux.Unlock()
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
