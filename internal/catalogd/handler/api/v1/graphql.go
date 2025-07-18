package v1

import (
	"net/http"

	"github.com/graphql-go/handler"
	"k8s.io/klog/v2"

	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
)

func apiV1GraphQLHandler(files storage.Files, indices storage.Indices, schemas storage.GraphQLSchemas) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		catalog := r.PathValue("catalog")
		logger := klog.FromContext(r.Context()).WithValues("catalog", catalog)

		catalogFile, err := files.Get(catalog)
		if err != nil {
			logger.Error(err, "error getting catalog file")
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		defer catalogFile.Close()

		catalogIndex, err := indices.Get(catalog)
		if err != nil {
			logger.Error(err, "error getting catalog index")
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		catalogSchema, err := schemas.Get(catalog)
		if err != nil {
			logger.Error(err, "error getting catalog graphql schema")
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		r = r.WithContext(storage.ContextWithCatalogData(r.Context(), catalogFile, catalogIndex))
		h := handler.New(&handler.Config{
			Schema:   catalogSchema,
			GraphiQL: true,
		})
		h.ServeHTTP(w, r)
	})
}
