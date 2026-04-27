package service

import (
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

func TestCachedGraphQLService_CacheHit(t *testing.T) {
	svc := NewCachedGraphQLService()

	// Create a test filesystem with valid catalog data
	testFS := fstest.MapFS{
		"catalog.json": &fstest.MapFile{
			Data: []byte(`{
				"schema": "olm.package",
				"name": "test-package",
				"defaultChannel": "stable"
			}`),
		},
	}

	// First call - cache miss, should build schema
	schema1, err := svc.GetSchema("test-catalog", testFS)
	if err != nil {
		t.Fatalf("First GetSchema failed: %v", err)
	}
	if schema1 == nil {
		t.Fatal("Expected non-nil schema")
	}

	// Second call - cache hit, should return same schema without rebuilding
	schema2, err := svc.GetSchema("test-catalog", testFS)
	if err != nil {
		t.Fatalf("Second GetSchema failed: %v", err)
	}
	if schema2 != schema1 {
		t.Error("Expected cache to return same schema instance")
	}
}

func TestCachedGraphQLService_InvalidateCache(t *testing.T) {
	svc := NewCachedGraphQLService()

	testFS := fstest.MapFS{
		"catalog.json": &fstest.MapFile{
			Data: []byte(`{
				"schema": "olm.package",
				"name": "test-package",
				"defaultChannel": "stable"
			}`),
		},
	}

	// Build and cache schema
	schema1, err := svc.GetSchema("test-catalog", testFS)
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
	schema2, err := svc.GetSchema("test-catalog", testFS)
	if err != nil {
		t.Fatalf("GetSchema after invalidation failed: %v", err)
	}
	if schema2 == schema1 {
		t.Error("Expected new schema instance after cache invalidation")
	}
}

func TestCachedGraphQLService_ConcurrentAccess(t *testing.T) {
	svc := NewCachedGraphQLService()

	testFS := fstest.MapFS{
		"catalog.json": &fstest.MapFile{
			Data: []byte(`{
				"schema": "olm.package",
				"name": "test-package",
				"defaultChannel": "stable"
			}`),
		},
	}

	// Run multiple concurrent GetSchema calls
	const concurrency = 20
	var wg sync.WaitGroup
	errors := make(chan error, concurrency)
	schemas := make(chan *interface{}, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			schema, err := svc.GetSchema("test-catalog", testFS)
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
	svc := NewCachedGraphQLService()

	// Track build count with a slow filesystem that takes time to build
	var buildCount int
	var buildMux sync.Mutex

	slowFS := &slowBuildFS{
		delay: 50 * time.Millisecond,
		onBuild: func() {
			buildMux.Lock()
			buildCount++
			buildMux.Unlock()
		},
		fs: fstest.MapFS{
			"catalog.json": &fstest.MapFile{
				Data: []byte(`{
					"schema": "olm.package",
					"name": "test-package",
					"defaultChannel": "stable"
				}`),
			},
		},
	}

	// Launch concurrent builds
	const concurrency = 10
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.GetSchema("test-catalog", slowFS)
		}()
	}

	wg.Wait()

	// Singleflight should deduplicate - only 1 build should occur
	buildMux.Lock()
	finalCount := buildCount
	buildMux.Unlock()

	if finalCount != 1 {
		t.Errorf("Expected singleflight to deduplicate builds (1 build), got %d builds", finalCount)
	}
}

func TestCachedGraphQLService_MultipleCatalogs(t *testing.T) {
	svc := NewCachedGraphQLService()

	fs1 := fstest.MapFS{
		"catalog.json": &fstest.MapFile{
			Data: []byte(`{"schema": "olm.package", "name": "catalog1"}`),
		},
	}
	fs2 := fstest.MapFS{
		"catalog.json": &fstest.MapFile{
			Data: []byte(`{"schema": "olm.package", "name": "catalog2"}`),
		},
	}

	// Build schemas for two different catalogs
	schema1, err := svc.GetSchema("catalog1", fs1)
	if err != nil {
		t.Fatalf("GetSchema for catalog1 failed: %v", err)
	}

	schema2, err := svc.GetSchema("catalog2", fs2)
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
	svc := NewCachedGraphQLService()

	testFS := fstest.MapFS{
		"catalog.json": &fstest.MapFile{
			Data: []byte(`{
				"schema": "olm.package",
				"name": "test-package",
				"defaultChannel": "stable"
			}`),
		},
	}

	// Execute a simple introspection query
	query := `{ __schema { queryType { name } } }`
	result, err := svc.ExecuteQuery("test-catalog", testFS, query)
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

// slowBuildFS wraps an fs.FS and adds a delay when Open is called
// to simulate slow schema building
type slowBuildFS struct {
	delay   time.Duration
	onBuild func()
	fs      fstest.MapFS
	built   bool
	mux     sync.Mutex
}

func (s *slowBuildFS) Open(name string) (fs.File, error) {
	s.mux.Lock()
	// Track that a build is happening (only once per instance)
	if !s.built {
		if s.onBuild != nil {
			s.onBuild()
		}
		// Simulate slow build
		time.Sleep(s.delay)
		s.built = true
	}
	s.mux.Unlock()
	return s.fs.Open(name)
}
