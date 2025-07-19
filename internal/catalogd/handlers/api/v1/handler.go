package v1

import (
	"fmt"
	"net/http"

	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
)

type APIV1Handler struct {
	basePath string
	mux      *http.ServeMux
}

type APIV1HandlerOption func(*APIV1Handler)

func WithAllHandler(files *storage.Files) APIV1HandlerOption {
	return func(h *APIV1Handler) {
		h.addHandler("all", apiV1AllHandler(files))
	}
}

func WithMetasHandler(files *storage.Files, indices *storage.Indices) APIV1HandlerOption {
	return func(h *APIV1Handler) {
		h.addHandler("metas", apiV1MetasHandler(files, indices))
	}
}

func WithGraphQLHandler(files *storage.Files, indices *storage.Indices, schemas *storage.GraphQLSchemas) APIV1HandlerOption {
	return func(h *APIV1Handler) {
		h.addHandler("graphql", apiV1GraphQLHandler(files, indices, schemas))
	}
}

func NewAPIV1Handler(basePath string, opts ...APIV1HandlerOption) *APIV1Handler {
	h := &APIV1Handler{
		basePath: basePath,
		mux:      http.NewServeMux(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *APIV1Handler) SubPath() string {
	return fmt.Sprintf("/%s/{catalog}/api/v1/", h.basePath)
}

func (h *APIV1Handler) addHandler(handlerSubPath string, handler http.Handler) {
	h.mux.Handle(fmt.Sprintf("%s%s", h.SubPath(), handlerSubPath), handler)
}

func (h *APIV1Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}
