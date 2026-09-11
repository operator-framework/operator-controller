package server_test

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
	"go.uber.org/mock/gomock"

	"github.com/operator-framework/operator-controller/internal/catalogd/server"
	mockcatalogdserver "github.com/operator-framework/operator-controller/internal/testutil/mock/catalogdserver"
	mockcatalogdservice "github.com/operator-framework/operator-controller/internal/testutil/mock/catalogdservice"
)

var alwaysLeader = func() bool { return true }

func TestHandleV1GraphQL_MethodNotAllowed(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")
	store := mockcatalogdserver.NewMockCatalogStore(ctrl)
	graphqlSvc := mockcatalogdservice.NewMockGraphQLService(ctrl)

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled, alwaysLeader)
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

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled, alwaysLeader)
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

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled, alwaysLeader)
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

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled, alwaysLeader)
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

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled, alwaysLeader)
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

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled, alwaysLeader)
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

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled, alwaysLeader)
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

func TestHandleV1GraphQL_CatalogNotFoundError_Leader(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")

	store := mockcatalogdserver.NewMockCatalogStore(ctrl)

	graphqlSvc := mockcatalogdservice.NewMockGraphQLService(ctrl)
	graphqlSvc.EXPECT().ExecuteQuery(gomock.Any(), "test-catalog", "{ summary { totalSchemas } }").Return(nil, fs.ErrNotExist)

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled, alwaysLeader)
	handler := handlers.Handler()

	query := `{"query": "{ summary { totalSchemas } }"}`
	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(query))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Leader knows the catalog genuinely does not exist → 404
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestHandleV1GraphQL_CatalogNotFoundError_NonLeader(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")

	store := mockcatalogdserver.NewMockCatalogStore(ctrl)

	graphqlSvc := mockcatalogdservice.NewMockGraphQLService(ctrl)
	graphqlSvc.EXPECT().ExecuteQuery(gomock.Any(), "test-catalog", "{ summary { totalSchemas } }").Return(nil, fs.ErrNotExist)

	neverLeader := func() bool { return false }
	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled, neverLeader)
	handler := handlers.Handler()

	query := `{"query": "{ summary { totalSchemas } }"}`
	req := httptest.NewRequest(http.MethodPost, "/test-catalog/api/v1/graphql", strings.NewReader(query))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Non-leader may not have synced content yet → 503 with Retry-After
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
	if retryAfter := w.Header().Get("Retry-After"); retryAfter != "1" {
		t.Errorf("Expected Retry-After header '1', got '%s'", retryAfter)
	}
	if body := strings.TrimSpace(w.Body.String()); body != "catalog content not yet available" {
		t.Errorf("Expected body 'catalog content not yet available', got '%s'", body)
	}
}

func TestHandleV1GraphQL_ExecuteQueryError(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")

	store := mockcatalogdserver.NewMockCatalogStore(ctrl)

	graphqlSvc := mockcatalogdservice.NewMockGraphQLService(ctrl)
	graphqlSvc.EXPECT().ExecuteQuery(gomock.Any(), "test-catalog", "{ summary { totalSchemas } }").Return(nil, context.DeadlineExceeded)

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled, alwaysLeader)
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

	handlers := server.NewCatalogHandlers(store, graphqlSvc, rootURL, server.MetasHandlerDisabled, server.GraphQLQueriesEnabled, alwaysLeader)
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

func TestHandleV1Metas_InvalidIfModifiedSince(t *testing.T) {
	ctrl := gomock.NewController(t)
	rootURL, _ := url.Parse("http://localhost/")

	// Create a temporary file with catalog content to be returned by the mock.
	tmpFile, err := os.CreateTemp(t.TempDir(), "catalog-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	catalogData := `{"schema":"olm.package","name":"test-pkg"}` + "\n"
	if _, err := tmpFile.WriteString(catalogData); err != nil {
		t.Fatal(err)
	}
	// Seek back to the beginning so the handler can read it.
	if _, err := tmpFile.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	info, err := tmpFile.Stat()
	if err != nil {
		t.Fatal(err)
	}

	store := mockcatalogdserver.NewMockCatalogStore(ctrl)
	store.EXPECT().GetCatalogData("test-catalog").Return(tmpFile, info, nil)

	handlers := server.NewCatalogHandlers(store, nil, rootURL, server.MetasHandlerEnabled, server.GraphQLQueriesDisabled, alwaysLeader)
	handler := handlers.Handler()

	req := httptest.NewRequest(http.MethodGet, "/test-catalog/api/v1/metas", nil)
	req.Header.Set("If-Modified-Since", "this-is-not-a-valid-date")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// An invalid If-Modified-Since header must produce only a 500 error;
	// no catalog JSONL should be appended to the body.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "olm.package") {
		t.Errorf("Response body must not contain catalog JSONL data, got: %s", body)
	}
}
