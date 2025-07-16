package handlers

import (
	"io"
	"net/http"

	"github.com/go-logr/logr"
	"k8s.io/klog/v2"

	"github.com/operator-framework/operator-controller/internal/catalogd/handlers/internal/handlerutil"
	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
)

func V1MetasHandler(indexer *storage.Indexer) http.Handler {
	return handlerutil.AllowedMethodsHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for unexpected query parameters
		expectedParams := map[string]bool{
			"schema":  true,
			"package": true,
			"name":    true,
		}

		for param := range r.URL.Query() {
			if !expectedParams[param] {
				http.Error(w, "Invalid query parameters", http.StatusBadRequest)
				return
			}
		}

		catalog := r.PathValue("catalog")
		logger := klog.FromContext(r.Context()).WithValues("catalog", catalog)

		idx, err := indexer.GetIndex(catalog)
		if err != nil {
			logger.Error(err, "error getting index")
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		catalogFile := idx.All()
		catalogStat, err := idx.Stat()
		if err != nil {
			logger.Error(err, "error stat-ing index")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Last-Modified", catalogStat.ModTime().UTC().Format(http.TimeFormat))
		done, _ := handlerutil.CheckPreconditions(w, r, catalogStat.ModTime())
		if done {
			return
		}

		schema := r.URL.Query().Get("schema")
		pkg := r.URL.Query().Get("package")
		name := r.URL.Query().Get("name")

		if schema == "" && pkg == "" && name == "" {
			// If no parameters are provided, return the entire catalog (this is the same as /api/v1/all)
			serveJSONLines(logger, w, r, catalogFile)
			return
		}
		indexReader, err := idx.Lookup(schema, pkg, name)
		if err != nil {
			logger.Error(err, "error looking up index")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		serveJSONLines(logger, w, r, indexReader)
	}), http.MethodGet, http.MethodHead)
}

func serveJSONLines(logger logr.Logger, w http.ResponseWriter, r *http.Request, rs io.Reader) {
	w.Header().Add("Content-Type", "application/jsonl")
	// Copy the content of the reader to the response writer
	// only if it's a Get request
	if r.Method == http.MethodHead {
		return
	}
	_, err := io.Copy(w, rs)
	if err != nil {
		logger.Error(err, "error copying request body")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
