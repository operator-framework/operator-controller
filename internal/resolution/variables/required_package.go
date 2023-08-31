package variables

import (
	"fmt"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
)

var _ deppy.Variable = &RequiredPackageVariable{}

type RequiredPackageVariable struct {
	*input.SimpleVariable
	bundleVariables []*BundleVariable
}

func (r *RequiredPackageVariable) BundleVariables() []*BundleVariable {
	return r.bundleVariables
}

func NewRequiredPackageVariable(packageName string, bundleVariables []*BundleVariable) *RequiredPackageVariable {
	id := deppy.IdentifierFromString(fmt.Sprintf("required package %s", packageName))
	variableIDs := make([]deppy.Identifier, 0, len(bundleVariables))
	for _, bundle := range bundleVariables {
		variableIDs = append(variableIDs, bundle.ID)
	}
	return &RequiredPackageVariable{
		SimpleVariable:  input.NewSimpleVariable(id, constraint.Mandatory(), constraint.Dependency(variableIDs...)),
		bundleVariables: bundleVariables,
	}
}
