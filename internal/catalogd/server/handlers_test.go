package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"

	gql "github.com/operator-framework/operator-controller/internal/catalogd/graphql"
)

// mockCatalogStore implements CatalogStore for testing
type mockCatalogStore struct {
	catalogFile *os.File
	catalogStat os.FileInfo
	catalogFS   fs.FS
	getDataErr  error
	getFSErr    error
}

func (m *mockCatalogStore) GetCatalogData(catalog string) (*os.File, os.FileInfo, error) {
	return m.catalogFile, m.catalogStat, m.getDataErr
}

func (m *mockCatalogStore) GetCatalogFS(catalog string) (fs.FS, error) {
	return m.catalogFS, m.getFSErr
}

func (m *mockCatalogStore) GetIndex(catalog string) (Index, error) {
	return nil, nil
}

// mockGraphQLService implements service.GraphQLService for testing
type mockGraphQLService struct {
	executeResult *graphql.Result
	executeErr    error
}

func (m *mockGraphQLService) GetSchema(catalog string, catalogFS fs.FS) (*gql.DynamicSchema, error) {
	return nil, nil
}

func (m *mockGraphQLService) ExecuteQuery(catalog string, catalogFS fs.FS, query string) (*graphql.Result, error) {
	return m.executeResult, m.executeErr
}

func (m *mockGraphQLService) InvalidateCache(catalog string) {}

func TestHandleV1GraphQL_MethodNotAllowed(t *testing.T) {
	rootURL, _ := url.Parse("http://localhost/")
	store := &mockCatalogStore{}
	graphqlSvc := &mockGraphQLService{}

	handlers := NewCatalogHandlers(store, graphqlSvc, rootURL, MetasHandlerDisabled, GraphQLQueriesEnabled)

	req := httptest.NewRequest(http.MethodGet, "/test-catalog/api/v1/graphql", nil)
	req.SetPathValue("catalog", "test-catalog")
	w := httptest.NewRecorder()

	handlers.handleV1GraphQL(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}

func TestHandleV1GraphQL_InvalidCatalogName(t *testing.T) {
	rootURL, _ := url.Parse("http://localhost/")
	store := &mockCatalogStore{}
	graphqlSvc := &mockGraphQLService{}

	handlers := NewCatalogHandlers(store, graphqlSvc, rootURL, MetasHandlerDisabled, GraphQLQueriesEnabled)

	req := httptest.NewRequest(http.MethodPost, "/INVALID-CATALOG-NAME/api/v1/graphql", strings.NewReader(`{"query": "{ summary { totalSchemas } }"}`))
	req.SetPathValue("catalog", "INVALID-CATALOG-NAME")
	w := httptest.NewRecorder()

	handlers.handleV1GraphQL(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleV1GraphQL_InvalidJSON(t *testing.T) {
	rootURL, _ := url.Parse("http://localhost/")
	store := &mockCatalogStore{}
	graphqlSvc := &mockGraphQLService{}

	handlers := NewCatalogHandlers(store, graphqlSvc, rootURL, MetasHandlerDisabled, GraphQLQueriesEnabled)

	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(`{invalid json`))
	req.SetPathValue("catalog", "test-catalog")
	w := httptest.NewRecorder()

	handlers.handleV1GraphQL(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleV1GraphQL_EmptyQuery(t *testing.T) {
	rootURL, _ := url.Parse("http://localhost/")
	store := &mockCatalogStore{}
	graphqlSvc := &mockGraphQLService{}

	handlers := NewCatalogHandlers(store, graphqlSvc, rootURL, MetasHandlerDisabled, GraphQLQueriesEnabled)

	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(`{"query": ""}`))
	req.SetPathValue("catalog", "test-catalog")
	w := httptest.NewRecorder()

	handlers.handleV1GraphQL(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	if !strings.Contains(w.Body.String(), "Query cannot be empty") {
		t.Errorf("Expected error message about empty query, got: %s", w.Body.String())
	}
}

func TestHandleV1GraphQL_QueryTooLarge(t *testing.T) {
	rootURL, _ := url.Parse("http://localhost/")
	store := &mockCatalogStore{}
	graphqlSvc := &mockGraphQLService{}

	handlers := NewCatalogHandlers(store, graphqlSvc, rootURL, MetasHandlerDisabled, GraphQLQueriesEnabled)

	// Create a query larger than 100KB
	largeQuery := strings.Repeat("a", 100001)
	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(`{"query": "`+largeQuery+`"}`))
	req.SetPathValue("catalog", "test-catalog")
	w := httptest.NewRecorder()

	handlers.handleV1GraphQL(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleV1GraphQL_BodyTooLarge(t *testing.T) {
	rootURL, _ := url.Parse("http://localhost/")
	store := &mockCatalogStore{}
	graphqlSvc := &mockGraphQLService{}

	handlers := NewCatalogHandlers(store, graphqlSvc, rootURL, MetasHandlerDisabled, GraphQLQueriesEnabled)

	// Create a body larger than 1MB
	largeBody := strings.Repeat("a", 1<<20+1)
	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(largeBody))
	req.SetPathValue("catalog", "test-catalog")
	w := httptest.NewRecorder()

	handlers.handleV1GraphQL(w, req)

	// MaxBytesReader should cause this to fail during decode
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleV1GraphQL_Success(t *testing.T) {
	rootURL, _ := url.Parse("http://localhost/")

	// Create a temporary directory for the mock filesystem
	tmpDir := t.TempDir()
	catalogFS := os.DirFS(tmpDir)

	store := &mockCatalogStore{
		catalogFS: catalogFS,
	}

	expectedResult := &graphql.Result{
		Data: map[string]interface{}{
			"summary": map[string]interface{}{
				"totalSchemas": 3,
			},
		},
	}

	graphqlSvc := &mockGraphQLService{
		executeResult: expectedResult,
	}

	handlers := NewCatalogHandlers(store, graphqlSvc, rootURL, MetasHandlerDisabled, GraphQLQueriesEnabled)

	query := `{"query": "{ summary { totalSchemas } }"}`
	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(query))
	req.SetPathValue("catalog", "test-catalog")
	w := httptest.NewRecorder()

	handlers.handleV1GraphQL(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify the result structure
	data, ok := result["data"].(map[string]interface{})
	if !ok {
		t.Error("Expected data field in response")
	}
	summary, ok := data["summary"].(map[string]interface{})
	if !ok {
		t.Error("Expected summary field in data")
	}
	totalSchemas, ok := summary["totalSchemas"].(float64) // JSON numbers decode to float64
	if !ok || totalSchemas != 3 {
		t.Errorf("Expected totalSchemas to be 3, got %v", summary["totalSchemas"])
	}
}

func TestHandleV1GraphQL_GetCatalogFSError(t *testing.T) {
	rootURL, _ := url.Parse("http://localhost/")

	store := &mockCatalogStore{
		getFSErr: fs.ErrNotExist,
	}

	graphqlSvc := &mockGraphQLService{}

	handlers := NewCatalogHandlers(store, graphqlSvc, rootURL, MetasHandlerDisabled, GraphQLQueriesEnabled)

	query := `{"query": "{ summary { totalSchemas } }"}`
	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(query))
	req.SetPathValue("catalog", "test-catalog")
	w := httptest.NewRecorder()

	handlers.handleV1GraphQL(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestHandleV1GraphQL_ExecuteQueryError(t *testing.T) {
	rootURL, _ := url.Parse("http://localhost/")

	tmpDir := t.TempDir()
	catalogFS := os.DirFS(tmpDir)

	store := &mockCatalogStore{
		catalogFS: catalogFS,
	}

	graphqlSvc := &mockGraphQLService{
		executeErr: context.DeadlineExceeded,
	}

	handlers := NewCatalogHandlers(store, graphqlSvc, rootURL, MetasHandlerDisabled, GraphQLQueriesEnabled)

	query := `{"query": "{ summary { totalSchemas } }"}`
	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(query))
	req.SetPathValue("catalog", "test-catalog")
	w := httptest.NewRecorder()

	handlers.handleV1GraphQL(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestAllowedMethodsHandler_POSTOnlyForGraphQL(t *testing.T) {
	rootURL, _ := url.Parse("http://localhost/")
	store := &mockCatalogStore{}
	graphqlSvc := &mockGraphQLService{}

	handlers := NewCatalogHandlers(store, graphqlSvc, rootURL, MetasHandlerDisabled, GraphQLQueriesEnabled)
	handler := handlers.Handler()

	// Test POST to GraphQL endpoint - should be allowed
	graphqlReq := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", bytes.NewReader([]byte(`{"query": "{ summary { totalSchemas } }"}`)))
	graphqlReq.SetPathValue("catalog", "test-catalog")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, graphqlReq)

	// Should not return 405 Method Not Allowed at the router level
	// (handler itself returns 405 for GET, but router allows POST through)
	if w.Code == http.StatusMethodNotAllowed && strings.Contains(w.Body.String(), "Method Not Allowed") {
		t.Error("POST should be allowed for GraphQL endpoint at router level")
	}
}
