package handler

import (
	"net/http"

	"github.com/operator-framework/operator-controller/internal/catalogd/handler/middleware"
)

type SubPathHandler interface {
	http.Handler
	SubPath() string
}

func NewSubPathHandler(subPath string, h http.Handler) SubPathHandler {
	return &simpleSubPathHandler{
		handler: h,
		subPath: subPath,
	}
}

type simpleSubPathHandler struct {
	handler http.Handler
	subPath string
}

func (h *simpleSubPathHandler) SubPath() string {
	return h.subPath
}

func (h *simpleSubPathHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.handler.ServeHTTP(w, r)
}

func NewStandardHandler(handlers ...SubPathHandler) http.Handler {
	mux := http.NewServeMux()
	for _, h := range handlers {
		mux.Handle(h.SubPath(), h)
	}
	return middleware.Standard(mux)
}
