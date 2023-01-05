package resolution

import (
	"context"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
)

// this package is a placeholder for deppy/resolution related code
// it is importing deppy to keep the library imported into the project
// and not bumped due to lack of imports. It will be removed once we start
// building out the olm deppy framework implementation here

var _ input.EntitySource = &SomeEntitySource{}

type SomeEntitySource struct {
}

func (s SomeEntitySource) Get(ctx context.Context, id deppy.Identifier) *input.Entity {
	//TODO implement me
	panic("implement me")
}

func (s SomeEntitySource) Filter(ctx context.Context, filter input.Predicate) (input.EntityList, error) {
	//TODO implement me
	panic("implement me")
}

func (s SomeEntitySource) GroupBy(ctx context.Context, fn input.GroupByFunction) (input.EntityListMap, error) {
	//TODO implement me
	panic("implement me")
}

func (s SomeEntitySource) Iterate(ctx context.Context, fn input.IteratorFunction) error {
	//TODO implement me
	panic("implement me")
}
