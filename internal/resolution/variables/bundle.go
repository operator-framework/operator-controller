package variables

import (
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
)

var _ deppy.Variable = &BundleVariable{}

type BundleVariable struct {
	*input.SimpleVariable
	bundleEntity *olmentity.BundleEntity
	dependencies []*olmentity.BundleEntity
}

func (b *BundleVariable) BundleEntity() *olmentity.BundleEntity {
	return b.bundleEntity
}

func (b *BundleVariable) Dependencies() []*olmentity.BundleEntity {
	return b.dependencies
}

func NewBundleVariable(bundleEntity *olmentity.BundleEntity, dependencyBundleEntities []*olmentity.BundleEntity) *BundleVariable {
	dependencyIDs := make([]deppy.Identifier, 0, len(dependencyBundleEntities))
	for _, bundle := range dependencyBundleEntities {
		dependencyIDs = append(dependencyIDs, bundle.ID)
	}
	var constraints []deppy.Constraint
	if len(dependencyIDs) > 0 {
		constraints = append(constraints, constraint.Dependency(dependencyIDs...))
	}
	return &BundleVariable{
		SimpleVariable: input.NewSimpleVariable(bundleEntity.ID, constraints...),
		bundleEntity:   bundleEntity,
		dependencies:   dependencyBundleEntities,
	}
}

var _ deppy.Variable = &BundleUniquenessVariable{}

type BundleUniquenessVariable struct {
	*input.SimpleVariable
}

// NewBundleUniquenessVariable creates a new variable that instructs the resolver to choose at most a single bundle
// from the input 'atMostID'. Examples:
// 1. restrict the solution to at most a single bundle per package
// 2. restrict the solution to at most a single bundler per provided gvk
// this guarantees that no two operators provide the same gvk and no two version of the same operator are running at the same time
func NewBundleUniquenessVariable(id deppy.Identifier, atMostIDs ...deppy.Identifier) *BundleUniquenessVariable {
	return &BundleUniquenessVariable{
		SimpleVariable: input.NewSimpleVariable(id, constraint.AtMost(1, atMostIDs...)),
	}
}
