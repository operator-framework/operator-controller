package graphql

import (
	"context"
	"os"
	"sync"

	"github.com/itchyny/gojq"

	"github.com/operator-framework/operator-controller/internal/catalogd/storage/index"
)

type contextKey string

const (
	contextKeyCatalogFile  contextKey = "catalogFile"
	contextKeyCatalogIndex contextKey = "catalogIndex"
	contextKeyJQCode       contextKey = "jqCode"
)

func ContextWithCatalogData(ctx context.Context, catalogFile *os.File, catalogIndex *index.Index) context.Context {
	ctx = context.WithValue(ctx, contextKeyCatalogFile, catalogFile)
	ctx = context.WithValue(ctx, contextKeyCatalogIndex, catalogIndex)
	ctx = context.WithValue(ctx, contextKeyJQCode, newJQCodeMap())
	return ctx
}

func fileFromContext(ctx context.Context) (*os.File, error) {
	v := ctx.Value(contextKeyCatalogFile)
	if v == nil {
		return nil, os.ErrNotExist
	}
	return v.(*os.File), nil
}

func indexFromContext(ctx context.Context) (*index.Index, error) {
	v := ctx.Value(contextKeyCatalogIndex)
	if v == nil {
		return nil, os.ErrNotExist
	}
	return v.(*index.Index), nil
}

func jqCodeFromContextOrCompileAndSet(ctx context.Context, query string) (*gojq.Code, error) {
	v := ctx.Value(contextKeyJQCode)
	if v == nil {
		return nil, os.ErrNotExist
	}
	return v.(*jqCodeMap).getOrCompileAndSet(query)
}

type jqCodeMap struct {
	m  map[string]*gojq.Code
	mu sync.RWMutex
}

func newJQCodeMap() *jqCodeMap {
	return &jqCodeMap{
		m: make(map[string]*gojq.Code),
	}
}

func (m *jqCodeMap) getOrCompileAndSet(query string) (*gojq.Code, error) {
	// Get a read lock to see if we already have the code compiled
	m.mu.RLock()
	jqCode, ok := m.m[query]
	m.mu.RUnlock()
	// If so, just return it
	if ok {
		return jqCode, nil
	}

	// If not, get a write lock so that we can compile the query
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check again to see if it was added between the time we let go
	// of the read lock and grabbed the write lock. If it was, just
	// return it.
	if jqCode, ok = m.m[query]; ok {
		return jqCode, nil
	}

	// Otherwise, now we really do need to compile it, and store it
	// in the map.
	parsed, err := gojq.Parse(query)
	if err != nil {
		return nil, err
	}
	jqCode, err = gojq.Compile(parsed)
	if err != nil {
		return nil, err
	}
	m.m[query] = jqCode
	return jqCode, nil
}
