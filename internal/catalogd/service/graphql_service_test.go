package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	gql "github.com/operator-framework/operator-controller/internal/catalogd/graphql"
)

// testCatalogDataProvider implements CatalogDataProvider for testing using in-memory metas
type testCatalogDataProvider struct {
	metas []*declcfg.Meta
}

func newTestProvider(metas []*declcfg.Meta) *testCatalogDataProvider {
	return &testCatalogDataProvider{metas: metas}
}

func (p *testCatalogDataProvider) LoadCatalogSchema(_ string) (*gql.CatalogSchema, error) {
	return gql.DiscoverSchemaFromMetas(p.metas)
}

func (p *testCatalogDataProvider) NewObjectLoader(_ string) (gql.ObjectLoader, error) {
	metasBySchema := make(map[string][]*declcfg.Meta)
	for _, meta := range p.metas {
		if meta.Schema != "" {
			metasBySchema[meta.Schema] = append(metasBySchema[meta.Schema], meta)
		}
	}
	return gql.NewInMemoryObjectLoader(metasBySchema), nil
}

// testMetas returns a slice of Meta objects for use in tests
func testMetas() []*declcfg.Meta {
	blob, _ := json.Marshal(map[string]interface{}{
		"schema":         "olm.package",
		"name":           "test-package",
		"defaultChannel": "stable",
	})
	return []*declcfg.Meta{
		{
			Schema: "olm.package",
			Name:   "test-package",
			Blob:   blob,
		},
	}
}

func TestCachedGraphQLService_CacheHit(t *testing.T) {
	provider := newTestProvider(testMetas())
	svc := NewCachedGraphQLService(provider)

	// First call - cache miss, should build schema
	schema1, err := svc.GetSchema(context.Background(), "test-catalog")
	if err != nil {
		t.Fatalf("First GetSchema failed: %v", err)
	}
	if schema1 == nil {
		t.Fatal("Expected non-nil schema")
	}

	// Second call - cache hit, should return same schema without rebuilding
	schema2, err := svc.GetSchema(context.Background(), "test-catalog")
	if err != nil {
		t.Fatalf("Second GetSchema failed: %v", err)
	}
	if schema2 != schema1 {
		t.Error("Expected cache to return same schema instance")
	}
}

func TestCachedGraphQLService_InvalidateCache(t *testing.T) {
	provider := newTestProvider(testMetas())
	svc := NewCachedGraphQLService(provider)

	// Build and cache schema
	schema1, err := svc.GetSchema(context.Background(), "test-catalog")
	if err != nil {
		t.Fatalf("GetSchema failed: %v", err)
	}

	// Invalidate cache
	svc.InvalidateCache("test-catalog")

	// Verify cache was cleared
	svc.schemaMux.RLock()
	_, exists := svc.schemaCache["test-catalog"]
	svc.schemaMux.RUnlock()

	if exists {
		t.Error("Expected cache to be cleared after InvalidateCache")
	}

	// Next call should rebuild
	schema2, err := svc.GetSchema(context.Background(), "test-catalog")
	if err != nil {
		t.Fatalf("GetSchema after invalidation failed: %v", err)
	}
	if schema2 == schema1 {
		t.Error("Expected new schema instance after cache invalidation")
	}
}

func TestCachedGraphQLService_ConcurrentAccess(t *testing.T) {
	provider := newTestProvider(testMetas())
	svc := NewCachedGraphQLService(provider)

	// Run multiple concurrent GetSchema calls
	const concurrency = 20
	var wg sync.WaitGroup
	errors := make(chan error, concurrency)
	schemas := make(chan *interface{}, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			schema, err := svc.GetSchema(context.Background(), "test-catalog")
			if err != nil {
				errors <- err
				return
			}
			// Store schema pointer as interface{} to compare instances
			var schemaPtr interface{} = schema
			schemas <- &schemaPtr
		}()
	}

	wg.Wait()
	close(errors)
	close(schemas)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent GetSchema failed: %v", err)
	}

	// All goroutines should get the same cached schema instance
	var firstSchema *interface{}
	schemaCount := 0
	for schema := range schemas {
		schemaCount++
		if firstSchema == nil {
			firstSchema = schema
		} else if *schema != *firstSchema {
			t.Error("Expected all concurrent calls to return same schema instance")
		}
	}

	if schemaCount != concurrency {
		t.Errorf("Expected %d schemas, got %d", concurrency, schemaCount)
	}
}

func TestCachedGraphQLService_SingleflightDeduplication(t *testing.T) {
	// Track schema build attempts using a counting provider
	var buildCount int
	var buildMux sync.Mutex

	metas := testMetas()
	countingProvider := &countingCatalogDataProvider{
		metas:    metas,
		buildMux: &buildMux,
		count:    &buildCount,
		delay:    50 * time.Millisecond,
	}

	// Create a service with the counting provider
	svc := NewCachedGraphQLService(countingProvider)

	// Launch concurrent GetSchema calls that will race to build
	const concurrency = 10
	var wg sync.WaitGroup
	var errors []error
	var errorsMux sync.Mutex

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.GetSchema(context.Background(), "test-catalog")
			if err != nil {
				errorsMux.Lock()
				errors = append(errors, err)
				errorsMux.Unlock()
			}
		}()
	}

	wg.Wait()

	// Check for errors
	if len(errors) > 0 {
		t.Fatalf("Schema builds failed: %v", errors)
	}

	// Singleflight should deduplicate - only 1 build should occur
	buildMux.Lock()
	finalCount := buildCount
	buildMux.Unlock()

	if finalCount != 1 {
		t.Errorf("Expected singleflight to deduplicate builds (1 build), got %d builds", finalCount)
	}
}

// countingCatalogDataProvider wraps testCatalogDataProvider to count and delay LoadCatalogSchema calls
type countingCatalogDataProvider struct {
	metas    []*declcfg.Meta
	buildMux *sync.Mutex
	count    *int
	delay    time.Duration
}

func (p *countingCatalogDataProvider) LoadCatalogSchema(_ string) (*gql.CatalogSchema, error) {
	p.buildMux.Lock()
	*p.count++
	p.buildMux.Unlock()

	// Simulate slow build
	time.Sleep(p.delay)

	return gql.DiscoverSchemaFromMetas(p.metas)
}

func (p *countingCatalogDataProvider) NewObjectLoader(_ string) (gql.ObjectLoader, error) {
	metasBySchema := make(map[string][]*declcfg.Meta)
	for _, meta := range p.metas {
		if meta.Schema != "" {
			metasBySchema[meta.Schema] = append(metasBySchema[meta.Schema], meta)
		}
	}
	return gql.NewInMemoryObjectLoader(metasBySchema), nil
}

func TestCachedGraphQLService_MultipleCatalogs(t *testing.T) {
	provider := newTestProvider(testMetas())
	svc := NewCachedGraphQLService(provider)

	// Build schemas for two different catalogs
	schema1, err := svc.GetSchema(context.Background(), "catalog1")
	if err != nil {
		t.Fatalf("GetSchema for catalog1 failed: %v", err)
	}

	schema2, err := svc.GetSchema(context.Background(), "catalog2")
	if err != nil {
		t.Fatalf("GetSchema for catalog2 failed: %v", err)
	}

	// Schemas should be different instances
	if schema1 == schema2 {
		t.Error("Expected different schemas for different catalogs")
	}

	// Both should be cached independently
	svc.schemaMux.RLock()
	_, exists1 := svc.schemaCache["catalog1"]
	_, exists2 := svc.schemaCache["catalog2"]
	svc.schemaMux.RUnlock()

	if !exists1 || !exists2 {
		t.Error("Expected both catalogs to be cached independently")
	}

	// Invalidate only catalog1
	svc.InvalidateCache("catalog1")

	svc.schemaMux.RLock()
	_, exists1AfterInvalidate := svc.schemaCache["catalog1"]
	_, exists2AfterInvalidate := svc.schemaCache["catalog2"]
	svc.schemaMux.RUnlock()

	if exists1AfterInvalidate {
		t.Error("Expected catalog1 to be removed from cache")
	}
	if !exists2AfterInvalidate {
		t.Error("Expected catalog2 to remain in cache")
	}
}

func TestCachedGraphQLService_ExecuteQuery(t *testing.T) {
	provider := newTestProvider(testMetas())
	svc := NewCachedGraphQLService(provider)

	// Execute a simple introspection query
	query := `{ __schema { queryType { name } } }`
	result, err := svc.ExecuteQuery(context.Background(), "test-catalog", query)
	if err != nil {
		t.Fatalf("ExecuteQuery failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Verify no GraphQL errors
	if len(result.Errors) > 0 {
		t.Errorf("Expected no GraphQL errors, got: %v", result.Errors)
	}

	// Verify result has data
	if result.Data == nil {
		t.Error("Expected result to have data")
	}
}
