package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
	"go.uber.org/mock/gomock"

	"github.com/operator-framework/operator-controller/internal/catalogd/server"
	mockcatalogdserver "github.com/operator-framework/operator-controller/internal/testutil/mock/catalogdserver"
	mockcatalogdservice "github.com/operator-framework/operator-controller/internal/testutil/mock/catalogdservice"
)

func TestHandleV1GraphQL_MethodNotAllowed(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")
	store := mockcatalogdserver.NewMockCatalogStore(ctrl)
	graphqlSvc := mockcatalogdservice.NewMockGraphQLService(ctrl)

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled)
	handler := handlers.Handler()

	req := httptest.NewRequest(http.MethodGet, "/test-catalog/api/v1/graphql", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}

func TestHandleV1GraphQL_InvalidCatalogName(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")
	store := mockcatalogdserver.NewMockCatalogStore(ctrl)
	graphqlSvc := mockcatalogdservice.NewMockGraphQLService(ctrl)

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled)
	handler := handlers.Handler()

	req := httptest.NewRequest(http.MethodPost, "/INVALID-CATALOG-NAME/api/v1/graphql", strings.NewReader(`{"query": "{ summary { totalSchemas } }"}`))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleV1GraphQL_InvalidJSON(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")
	store := mockcatalogdserver.NewMockCatalogStore(ctrl)
	graphqlSvc := mockcatalogdservice.NewMockGraphQLService(ctrl)

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled)
	handler := handlers.Handler()

	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(`{invalid json`))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleV1GraphQL_EmptyQuery(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")
	store := mockcatalogdserver.NewMockCatalogStore(ctrl)
	graphqlSvc := mockcatalogdservice.NewMockGraphQLService(ctrl)

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled)
	handler := handlers.Handler()

	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(`{"query": ""}`))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	if !strings.Contains(w.Body.String(), "Query cannot be empty") {
		t.Errorf("Expected error message about empty query, got: %s", w.Body.String())
	}
}

func TestHandleV1GraphQL_QueryTooLarge(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")
	store := mockcatalogdserver.NewMockCatalogStore(ctrl)
	graphqlSvc := mockcatalogdservice.NewMockGraphQLService(ctrl)

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled)
	handler := handlers.Handler()

	// Create a query larger than 100KB
	largeQuery := strings.Repeat("a", 100001)
	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(`{"query": "`+largeQuery+`"}`))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleV1GraphQL_BodyTooLarge(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")
	store := mockcatalogdserver.NewMockCatalogStore(ctrl)
	graphqlSvc := mockcatalogdservice.NewMockGraphQLService(ctrl)

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled)
	handler := handlers.Handler()

	// Create a body larger than 1MB
	largeBody := strings.Repeat("a", 1<<20+1)
	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(largeBody))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// MaxBytesReader should cause this to fail during decode
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleV1GraphQL_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")

	store := mockcatalogdserver.NewMockCatalogStore(ctrl)

	expectedResult := &graphql.Result{
		Data: map[string]interface{}{
			"summary": map[string]interface{}{
				"totalSchemas": 3,
			},
		},
	}

	graphqlSvc := mockcatalogdservice.NewMockGraphQLService(ctrl)
	graphqlSvc.EXPECT().ExecuteQuery(gomock.Any(), "test-catalog", "{ summary { totalSchemas } }").Return(expectedResult, nil)

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled)
	handler := handlers.Handler()

	query := `{"query": "{ summary { totalSchemas } }"}`
	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(query))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

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

func TestHandleV1GraphQL_CatalogNotFoundError(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")

	store := mockcatalogdserver.NewMockCatalogStore(ctrl)

	graphqlSvc := mockcatalogdservice.NewMockGraphQLService(ctrl)
	graphqlSvc.EXPECT().ExecuteQuery(gomock.Any(), "test-catalog", "{ summary { totalSchemas } }").Return(nil, fs.ErrNotExist)

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled)
	handler := handlers.Handler()

	query := `{"query": "{ summary { totalSchemas } }"}`
	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(query))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestHandleV1GraphQL_ExecuteQueryError(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")

	store := mockcatalogdserver.NewMockCatalogStore(ctrl)

	graphqlSvc := mockcatalogdservice.NewMockGraphQLService(ctrl)
	graphqlSvc.EXPECT().ExecuteQuery(gomock.Any(), "test-catalog", "{ summary { totalSchemas } }").Return(nil, context.DeadlineExceeded)

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled)
	handler := handlers.Handler()

	query := `{"query": "{ summary { totalSchemas } }"}`
	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(query))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestAllowedMethodsHandler_POSTOnlyForGraphQL(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")
	store := mockcatalogdserver.NewMockCatalogStore(ctrl)

	graphqlSvc := mockcatalogdservice.NewMockGraphQLService(ctrl)
	graphqlSvc.EXPECT().ExecuteQuery(gomock.Any(), "test-catalog", "{ summary { totalSchemas } }").Return(nil, nil)

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled)
	handler := handlers.Handler()

	// Test POST to GraphQL endpoint - should be allowed
	graphqlReq := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", bytes.NewReader([]byte(`{"query": "{ summary { totalSchemas } }"}`)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, graphqlReq)

	// Should not return 405 Method Not Allowed at the router level
	// (handler itself returns 405 for GET, but router allows POST through)
	if w.Code == http.StatusMethodNotAllowed && strings.Contains(w.Body.String(), "Method Not Allowed") {
		t.Error("POST should be allowed for GraphQL endpoint at router level")
	}
}
