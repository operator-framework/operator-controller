package v1

import (
	"io"
	"net/http"

	"k8s.io/klog/v2"

	"github.com/operator-framework/operator-controller/internal/catalogd/handler/internal/handlerutil"
	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
)

func apiV1MetasHandler(files storage.Files, indices storage.Indices) http.Handler {
	metasHander := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		catalogFile, err := files.Get(catalog)
		if err != nil {
			logger.Error(err, "error getting catalog file")
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		defer catalogFile.Close()

		catalogStat, err := catalogFile.Stat()
		if err != nil {
			logger.Error(err, "error stat-ing catalog file")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		catalogIndex, err := indices.Get(catalog)
		if err != nil {
			logger.Error(err, "error getting catalog index")
			http.Error(w, "Not found", http.StatusNotFound)
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
			serveJSONLines(w, r, catalogFile)
			return
		}
		indexReader := catalogIndex.Get(catalogFile, schema, pkg, name)
		serveJSONLines(w, r, indexReader)
	})
	return handlerutil.AllowedMethodsHandler(metasHander, http.MethodGet, http.MethodHead)
}

func serveJSONLines(w http.ResponseWriter, r *http.Request, rs io.Reader) {
	w.Header().Add("Content-Type", "application/jsonl")
	// Copy the content of the reader to the response writer
	// only if it's a Get request
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rs)
}
