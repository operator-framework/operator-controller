package v1

import (
	"net/http"

	"github.com/graphql-go/handler"
	"k8s.io/klog/v2"

	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
)

func apiV1GraphQLHandler(files *storage.Files, indices *storage.Indices, schemas *storage.GraphQLSchemas) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		catalog := r.PathValue("catalog")
		logger := klog.FromContext(r.Context()).WithValues("catalog", catalog)

		file, err := files.Get(catalog)
		if err != nil {
			logger.Error(err, "error getting catalog file")
			http.Error(w, "Not found", http.StatusNotFound)
		}
		defer file.Close()

		idx, err := indices.Get(catalog)
		if err != nil {
			logger.Error(err, "error getting catalog index")
		}

		schema, err := schemas.Get(catalog)
		if err != nil {
			logger.Error(err, "error getting catalog graphql schema")
			http.Error(w, "Not found", http.StatusNotFound)
		}

		r = r.WithContext(storage.ContextWithCatalogData(r.Context(), file, idx))
		h := handler.New(&handler.Config{
			Schema:   schema,
			GraphiQL: true,
		})
		h.ServeHTTP(w, r)
	})
}
