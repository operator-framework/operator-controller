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

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/operator-controller/internal/catalogd/service"
)

var errInvalidParams = errors.New("invalid parameters")

const timeFormat = "Mon, 02 Jan 2006 15:04:05 GMT"

// CatalogHandlers handles HTTP requests for catalog content
type CatalogHandlers struct {
	store         CatalogStore
	graphqlSvc    service.GraphQLService
	rootURL       *url.URL
	enableMetas   bool
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
func NewCatalogHandlers(store CatalogStore, graphqlSvc service.GraphQLService, rootURL *url.URL, enableMetas bool) *CatalogHandlers {
	return &CatalogHandlers{
		store:       store,
		graphqlSvc:  graphqlSvc,
		rootURL:     rootURL,
		enableMetas: enableMetas,
	}
}

// Handler returns an HTTP handler with all routes configured
func (h *CatalogHandlers) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(h.rootURL.JoinPath("{catalog}", "api", "v1", "all").Path, h.handleV1All)
	if h.enableMetas {
		mux.HandleFunc(h.rootURL.JoinPath("{catalog}", "api", "v1", "metas").Path, h.handleV1Metas)
	}
	mux.HandleFunc(h.rootURL.JoinPath("{catalog}", "api", "v1", "graphql").Path, h.handleV1GraphQL)

	return allowedMethodsHandler(mux, http.MethodGet, http.MethodHead, http.MethodPost)
}

// handleV1All serves the complete catalog content
func (h *CatalogHandlers) handleV1All(w http.ResponseWriter, r *http.Request) {
	catalog := r.PathValue("catalog")
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

	catalog := r.PathValue("catalog")
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

	catalog := r.PathValue("catalog")

	// Parse GraphQL query from request body
	var params struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
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
	switch {
	case errors.Is(err, fs.ErrNotExist):
		code = http.StatusNotFound
	case errors.Is(err, fs.ErrPermission):
		code = http.StatusForbidden
	case errors.Is(err, errInvalidParams):
		code = http.StatusBadRequest
	default:
		code = http.StatusInternalServerError
	}
	// Log the actual error for debugging
	fmt.Printf("HTTP Error %d: %v\n", code, err)
	http.Error(w, fmt.Sprintf("%d %s", code, http.StatusText(code)), code)
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

// allowedMethodsHandler wraps a handler to only allow specific HTTP methods
func allowedMethodsHandler(next http.Handler, allowedMethods ...string) http.Handler {
	allowedMethodSet := sets.New[string](allowedMethods...)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow POST requests only for GraphQL endpoint
		if r.URL.Path != "" && len(r.URL.Path) >= 7 && r.URL.Path[len(r.URL.Path)-7:] != "graphql" && r.Method == http.MethodPost {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if !allowedMethodSet.Has(r.Method) {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}
