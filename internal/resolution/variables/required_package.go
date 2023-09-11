package variables

import (
	"fmt"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

var _ deppy.Variable = &RequiredPackageVariable{}

type RequiredPackageVariable struct {
	*input.SimpleVariable
	bundles []*catalogmetadata.Bundle
}

func (r *RequiredPackageVariable) BundleEntities() []*catalogmetadata.Bundle {
	return r.bundles
}

func NewRequiredPackageVariable(packageName string, bundles []*catalogmetadata.Bundle) *RequiredPackageVariable {
	id := deppy.IdentifierFromString(fmt.Sprintf("required package %s", packageName))
	var variableIDs []deppy.Identifier
	for _, bundle := range bundles {
		variableIDs = append(variableIDs, BundleToBundleVariableIDs(bundle)...)
	}
	return &RequiredPackageVariable{
		SimpleVariable: input.NewSimpleVariable(id, constraint.Mandatory(), constraint.Dependency(variableIDs...)),
		bundles:        bundles,
	}
}
