package variables

import (
	"fmt"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

var _ deppy.Variable = &BundleVariable{}

type BundleVariable struct {
	*input.SimpleVariable
	bundle       *catalogmetadata.Bundle
	dependencies []*catalogmetadata.Bundle
}

func (b *BundleVariable) Bundle() *catalogmetadata.Bundle {
	return b.bundle
}

func (b *BundleVariable) Dependencies() []*catalogmetadata.Bundle {
	return b.dependencies
}

func NewBundleVariable(bundle *catalogmetadata.Bundle, dependencies []*catalogmetadata.Bundle) *BundleVariable {
	dependencyIDs := make([]deppy.Identifier, 0, len(dependencies))
	for _, dependency := range dependencies {
		dependencyIDs = append(dependencyIDs, BundleVariableID(dependency))
	}
	var constraints []deppy.Constraint
	if len(dependencyIDs) > 0 {
		constraints = append(constraints, constraint.Dependency(dependencyIDs...))
	}
	return &BundleVariable{
		SimpleVariable: input.NewSimpleVariable(BundleVariableID(bundle), constraints...),
		bundle:         bundle,
		dependencies:   dependencies,
	}
}

var _ deppy.Variable = &BundleUniquenessVariable{}

type BundleUniquenessVariable struct {
	ID                    deppy.Identifier
	UniquenessConstraints []deppy.Constraint
}

func (s *BundleUniquenessVariable) Identifier() deppy.Identifier {
	return s.ID
}

func (s *BundleUniquenessVariable) Constraints() []deppy.Constraint {
	return s.UniquenessConstraints
}

// NewBundleUniquenessVariable creates a new variable that instructs
// the resolver to choose at most a single bundle from the input 'bundleVarIDs'
func NewBundleUniquenessVariable(id deppy.Identifier, bundleVarIDs ...deppy.Identifier) *BundleUniquenessVariable {
	return &BundleUniquenessVariable{
		ID:                    id,
		UniquenessConstraints: []deppy.Constraint{constraint.AtMost(1, bundleVarIDs...)},
	}
}

// BundleVariableID returns an ID for a given bundle.
func BundleVariableID(bundle *catalogmetadata.Bundle) deppy.Identifier {
	return deppy.Identifier(
		fmt.Sprintf("%s-%s-%s", bundle.CatalogName, bundle.Package, bundle.Name),
	)
}
