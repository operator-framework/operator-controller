package handlers

import (
	"github.com/operator-framework/operator-controller/internal/catalogd/handlers/internal/handlerutil"
	"net/http"

	"k8s.io/klog/v2"

	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
)

func V1AllHandler(indexer *storage.Indexer) http.Handler {
	return handlerutil.AllowedMethodsHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		w.Header().Add("Content-Type", "application/jsonl")
		http.ServeContent(w, r, "", catalogStat.ModTime(), catalogFile)
	}), http.MethodGet, http.MethodHead)
}
