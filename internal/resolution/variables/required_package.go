package variables

import (
	"fmt"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
)

var _ deppy.Variable = &RequiredPackageVariable{}

type RequiredPackageVariable struct {
	*input.SimpleVariable
	bundleEntities []*olmentity.BundleEntity
}

func (r *RequiredPackageVariable) BundleEntities() []*olmentity.BundleEntity {
	return r.bundleEntities
}

func NewRequiredPackageVariable(packageName string, bundleEntities []*olmentity.BundleEntity) *RequiredPackageVariable {
	id := deppy.IdentifierFromString(fmt.Sprintf("required package %s", packageName))
	entityIDs := make([]deppy.Identifier, 0, len(bundleEntities))
	for _, bundle := range bundleEntities {
		entityIDs = append(entityIDs, bundle.ID)
	}
	return &RequiredPackageVariable{
		SimpleVariable: input.NewSimpleVariable(id, constraint.Mandatory(), constraint.Dependency(entityIDs...)),
		bundleEntities: bundleEntities,
	}
}
