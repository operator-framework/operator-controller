package v1

import (
	"net/http"

	"k8s.io/klog/v2"

	"github.com/operator-framework/operator-controller/internal/catalogd/handler/internal/handlerutil"
	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
)

func apiV1AllHandler(files storage.Files) http.Handler {
	allHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		w.Header().Add("Content-Type", "application/jsonl")
		http.ServeContent(w, r, "", catalogStat.ModTime(), catalogFile)
	})
	return handlerutil.AllowedMethodsHandler(allHandler, http.MethodGet, http.MethodHead)
}
