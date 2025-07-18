package v1

import (
	"fmt"
	"net/http"

	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
)

type APIV1Handler struct {
	basePath string
	mux      *http.ServeMux
	data     *storage.Instances
}

type APIV1HandlerOption func(*APIV1Handler)

func WithAllHandler(enabled bool) APIV1HandlerOption {
	return func(h *APIV1Handler) {
		if enabled {
			h.addHandler("all", apiV1AllHandler(h.data.Files()))
		}
	}
}

func WithMetasHandler(enabled bool) APIV1HandlerOption {
	return func(h *APIV1Handler) {
		if enabled {
			h.addHandler("metas", apiV1MetasHandler(h.data.Files(), h.data.Indices()))
		}
	}
}

func WithGraphQLHandler(enabled bool) APIV1HandlerOption {
	return func(h *APIV1Handler) {
		if enabled {
			h.addHandler("graphql", apiV1GraphQLHandler(h.data.Files(), h.data.Indices(), h.data.GraphQLSchemas()))
		}
	}
}

func NewAPIV1Handler(basePath string, data *storage.Instances, opts ...APIV1HandlerOption) *APIV1Handler {
	h := &APIV1Handler{
		basePath: basePath,
		mux:      http.NewServeMux(),
		data:     data,
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
