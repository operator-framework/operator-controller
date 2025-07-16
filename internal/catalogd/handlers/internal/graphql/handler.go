package graphql

import (
	"context"
	"fmt"
	"net/http"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/handler"

	"github.com/operator-framework/operator-registry/alpha/declcfg"

	"github.com/operator-framework/operator-controller/internal/catalogd/storage"
)

func NewHandler(ctx context.Context, index *storage.Index, metasChan <-chan *declcfg.Meta) (http.Handler, error) {
	schema, err := buildSchema(ctx, index, metasChan)
	if err != nil {
		return nil, fmt.Errorf("failed to build GraphQL schema: %w", err)
	}

	return handler.New(&handler.Config{
		Schema:   schema,
		GraphiQL: true,
	}), nil
}

func NewRequestContext(ctx context.Context) context.Context {
	return contextWithJQCodeMap(ctx)
}

func buildSchema(ctx context.Context, index *storage.Index, metasChan <-chan *declcfg.Meta) (*graphql.Schema, error) {
	// Process all Meta objects to accumulate field data and build index
	schemaGenerator := newGraphQLSchemaGenerator(index)

	if err := func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case meta, ok := <-metasChan:
				if !ok {
					return nil
				}
				if err := schemaGenerator.ProcessMeta(meta); err != nil {
					return fmt.Errorf("failed to process meta: %w", err)
				}
			}
		}
	}(); err != nil {
		return nil, err
	}

	// Generate the complete GraphQL schema from accumulated data
	schema, err := schemaGenerator.GenerateFBCSchema()
	if err != nil {
		return nil, fmt.Errorf("failed to generate GraphQL schema: %w", err)
	}
	return schema, nil
}
