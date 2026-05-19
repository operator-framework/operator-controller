package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"

	"github.com/operator-framework/operator-controller/internal/catalogd/service"
)

var (
	errInvalidParams      = errors.New("invalid parameters")
	errInvalidCatalogName = errors.New("invalid catalog name")
)

// MetasHandlerMode controls whether the metas API endpoint is enabled
type MetasHandlerMode bool

const (
	MetasHandlerDisabled MetasHandlerMode = false
	MetasHandlerEnabled  MetasHandlerMode = true
)

// GraphQLQueriesMode controls whether GraphQL queries are enabled
type GraphQLQueriesMode bool

const (
	GraphQLQueriesDisabled GraphQLQueriesMode = false
	GraphQLQueriesEnabled  GraphQLQueriesMode = true
)

// routeConfig defines allowed HTTP methods for a specific route
type routeConfig struct {
	path           string
	handler        http.HandlerFunc
	allowedMethods []string
}

// CatalogHandlers handles HTTP requests for catalog content
type CatalogHandlers struct {
	store         CatalogStore
	graphqlSvc    service.GraphQLService
	rootURL       *url.URL
	enableMetas   MetasHandlerMode
	enableGraphQL GraphQLQueriesMode
}

// Index provides methods for looking up catalog content by schema/package/name
type Index interface {
	Get(catalogFile io.ReaderAt, schema, pkg, name string) io.Reader
}

// CatalogStore defines the storage interface needed by handlers
type CatalogStore interface {
	// GetCatalogData returns the catalog file and its metadata
	GetCatalogData(catalog string) (*os.File, os.FileInfo, error)

	// GetCatalogFS returns a filesystem interface for the catalog
	GetCatalogFS(catalog string) (fs.FS, error)

	// GetIndex returns the index for a catalog (if metas handler is enabled)
	GetIndex(catalog string) (Index, error)
}

// NewCatalogHandlers creates a new HTTP handlers instance
func NewCatalogHandlers(store CatalogStore, graphqlSvc service.GraphQLService, rootURL *url.URL, enableMetas MetasHandlerMode, enableGraphQL GraphQLQueriesMode) *CatalogHandlers {
	return &CatalogHandlers{
		store:         store,
		graphqlSvc:    graphqlSvc,
		rootURL:       rootURL,
		enableMetas:   enableMetas,
		enableGraphQL: enableGraphQL,
	}
}

// Handler returns an HTTP handler with all routes configured
func (h *CatalogHandlers) Handler() http.Handler {
	// Build route configurations - each service contributes its routes and allowed methods
	routes := []routeConfig{
		{
			path:           h.rootURL.JoinPath("{catalog}", "api", "v1", "all").Path,
			handler:        h.handleV1All,
			allowedMethods: []string{http.MethodGet, http.MethodHead},
		},
	}

	if h.enableMetas {
		routes = append(routes, routeConfig{
			path:           h.rootURL.JoinPath("{catalog}", "api", "v1", "metas").Path,
			handler:        h.handleV1Metas,
			allowedMethods: []string{http.MethodGet, http.MethodHead},
		})
	}

	if h.enableGraphQL {
		routes = append(routes, routeConfig{
			path:           h.rootURL.JoinPath("{catalog}", "api", "v1", "graphql").Path,
			handler:        h.handleV1GraphQL,
			allowedMethods: []string{http.MethodPost},
		})
	}

	return h.buildRoutedHandler(routes)
}

// handleV1All serves the complete catalog content
func (h *CatalogHandlers) handleV1All(w http.ResponseWriter, r *http.Request) {
	catalog := r.PathValue("catalog")
	if err := isValidCatalogName(catalog); err != nil {
		httpError(w, err)
		return
	}
	catalogFile, catalogStat, err := h.store.GetCatalogData(catalog)
	if err != nil {
		httpError(w, err)
		return
	}
	defer catalogFile.Close()

	w.Header().Add("Content-Type", "application/jsonl")
	http.ServeContent(w, r, "", catalogStat.ModTime(), catalogFile)
}

// handleV1Metas serves filtered catalog content based on query parameters
func (h *CatalogHandlers) handleV1Metas(w http.ResponseWriter, r *http.Request) {
	catalog := r.PathValue("catalog")
	if err := isValidCatalogName(catalog); err != nil {
		httpError(w, err)
		return
	}

	// Check for unexpected query parameters
	expectedParams := map[string]bool{
		"schema":  true,
		"package": true,
		"name":    true,
	}

	for param := range r.URL.Query() {
		if !expectedParams[param] {
			httpError(w, errInvalidParams)
			return
		}
	}
	catalogFile, catalogStat, err := h.store.GetCatalogData(catalog)
	if err != nil {
		httpError(w, err)
		return
	}
	defer catalogFile.Close()

	w.Header().Set("Last-Modified", catalogStat.ModTime().UTC().Format(timeFormat))
	done := checkPreconditions(w, r, catalogStat.ModTime())
	if done {
		return
	}

	schema := r.URL.Query().Get("schema")
	pkg := r.URL.Query().Get("package")
	name := r.URL.Query().Get("name")

	if schema == "" && pkg == "" && name == "" {
		// If no parameters are provided, return the entire catalog
		serveJSONLines(w, r, catalogFile)
		return
	}

	idx, err := h.store.GetIndex(catalog)
	if err != nil {
		httpError(w, err)
		return
	}
	indexReader := idx.Get(catalogFile, schema, pkg, name)
	serveJSONLines(w, r, indexReader)
}

// handleV1GraphQL handles GraphQL queries
func (h *CatalogHandlers) handleV1GraphQL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST is allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.graphqlSvc == nil {
		http.Error(w, "GraphQL queries are not enabled", http.StatusServiceUnavailable)
		return
	}

	catalog := r.PathValue("catalog")
	if err := isValidCatalogName(catalog); err != nil {
		httpError(w, err)
		return
	}

	// Limit request body size to prevent memory exhaustion attacks (1MB limit)
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	// Parse GraphQL query from request body
	var params struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate query
	if params.Query == "" {
		http.Error(w, "Query cannot be empty", http.StatusBadRequest)
		return
	}
	if len(params.Query) > 100000 { // 100KB limit
		http.Error(w, "Query too large", http.StatusBadRequest)
		return
	}

	// Get catalog filesystem
	catalogFS, err := h.store.GetCatalogFS(catalog)
	if err != nil {
		httpError(w, err)
		return
	}

	// Execute GraphQL query through the service
	result, err := h.graphqlSvc.ExecuteQuery(catalog, catalogFS, params.Query)
	if err != nil {
		httpError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		httpError(w, err)
		return
	}
}

// httpError writes an HTTP error response based on the error type
func httpError(w http.ResponseWriter, err error) {
	var code int
	var message string
	switch {
	case errors.Is(err, fs.ErrNotExist):
		code = http.StatusNotFound
		message = fmt.Sprintf("%d %s", code, http.StatusText(code))
	case errors.Is(err, fs.ErrPermission):
		code = http.StatusForbidden
		message = fmt.Sprintf("%d %s", code, http.StatusText(code))
	case errors.Is(err, errInvalidParams):
		code = http.StatusBadRequest
		message = fmt.Sprintf("%d %s", code, http.StatusText(code))
	case errors.Is(err, errInvalidCatalogName):
		code = http.StatusBadRequest
		// Include detailed DNS1123 validation errors for better user feedback
		message = err.Error()
	default:
		code = http.StatusInternalServerError
		message = fmt.Sprintf("%d %s", code, http.StatusText(code))
	}
	// Log 5xx errors at ERROR level, 4xx at INFO level
	if code >= 500 {
		klog.ErrorS(err, "HTTP error", "code", code)
	} else {
		klog.V(2).InfoS("HTTP client error", "code", code, "error", err.Error())
	}
	http.Error(w, message, code)
}

// serveJSONLines writes JSON lines content to the response
func serveJSONLines(w http.ResponseWriter, r *http.Request, rs io.Reader) {
	w.Header().Add("Content-Type", "application/jsonl")
	// Copy the content of the reader to the response writer only if it's a GET request
	if r.Method == http.MethodHead {
		return
	}
	_, err := io.Copy(w, rs)
	if err != nil {
		httpError(w, err)
		return
	}
}

// buildRoutedHandler creates an HTTP handler from route configurations
// Each route specifies its own allowed methods, enabling service-dependent method restrictions
func (h *CatalogHandlers) buildRoutedHandler(routes []routeConfig) http.Handler {
	mux := http.NewServeMux()

	for _, route := range routes {
		// Wrap each handler with method checking specific to that route
		mux.HandleFunc(route.path, methodRestrictedHandler(route.handler, route.allowedMethods...))
	}

	return mux
}

// methodRestrictedHandler wraps a handler to only allow specific HTTP methods
func methodRestrictedHandler(handler http.HandlerFunc, allowedMethods ...string) http.HandlerFunc {
	allowedSet := sets.New(allowedMethods...)
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowedSet.Has(r.Method) {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		handler(w, r)
	}
}

// isValidCatalogName validates that a catalog name is safe for filesystem operations
// and suitable for Kubernetes metadata.name by using DNS1123 subdomain validation.
// Prevents path traversal attacks by requiring alphanumeric start/end characters.
// Returns nil if valid, or an error with detailed DNS1123 validation messages if invalid.
func isValidCatalogName(name string) error {
	errs := validation.IsDNS1123Subdomain(name)
	if len(errs) == 0 {
		return nil
	}
	// Wrap errInvalidCatalogName to maintain errors.Is compatibility while adding details
	return fmt.Errorf("%w: %s", errInvalidCatalogName, strings.Join(errs, "; "))
}
