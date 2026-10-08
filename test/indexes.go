package test

import (
	"context"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// WithIndexes registers controller cache indexes on a fake client builder.
// The builder must already have its scheme configured.
func WithIndexes(t *testing.T, builder *fake.ClientBuilder, setup func(context.Context, client.FieldIndexer) error) *fake.ClientBuilder {
	t.Helper()
	if err := setup(t.Context(), fakeFieldIndexer{builder: builder}); err != nil {
		t.Fatalf("setting up fake client indexes: %v", err)
	}
	return builder
}

type fakeFieldIndexer struct {
	builder *fake.ClientBuilder
}

func (f fakeFieldIndexer) IndexField(_ context.Context, obj client.Object, field string, extract client.IndexerFunc) error {
	f.builder.WithIndex(obj, field, extract)
	return nil
}
