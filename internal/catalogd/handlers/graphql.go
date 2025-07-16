package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"k8s.io/klog/v2"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/catalogd/handlers/internal/graphql"
	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
)

type GraphQLHandler struct {
	catalogHandlers map[string]http.Handler
	mu              sync.RWMutex
}

var (
	_ storage.MetaProcessor = (*GraphQLHandler)(nil)
	_ http.Handler          = (*GraphQLHandler)(nil)
)

func V1GraphQLHandler() *GraphQLHandler {
	return &GraphQLHandler{
		catalogHandlers: make(map[string]http.Handler),
	}
}

func (h *GraphQLHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	catalog := r.PathValue("catalog")
	logger := klog.FromContext(r.Context()).WithValues("catalog", catalog)

	h.mu.RLock()
	defer h.mu.RUnlock()

	handler, ok := h.catalogHandlers[catalog]
	if !ok {
		logger.Error(errors.New("no handler found for catalog"), "catalog not found")
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	r = r.WithContext(graphql.NewRequestContext(r.Context()))
	handler.ServeHTTP(w, r)
}

func (h *GraphQLHandler) ProcessMetas(ctx context.Context, catalog string, idx *storage.Index, metasChan <-chan *declcfg.Meta) error {
	handler, err := graphql.NewHandler(ctx, idx, metasChan)
	if err != nil {
		return fmt.Errorf("failed to create graphql handler: %w", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.catalogHandlers[catalog] = handler
	return nil
}
