package variables

import (
	"fmt"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
)

var _ deppy.Variable = &InstalledPackageVariable{}

type InstalledPackageVariable struct {
	*input.SimpleVariable
	bundleVariables []*BundleVariable
}

func (r *InstalledPackageVariable) BundleVariables() []*BundleVariable {
	return r.bundleVariables
}

func NewInstalledPackageVariable(packageName string, bundleVariables []*BundleVariable) *InstalledPackageVariable {
	id := deppy.IdentifierFromString(fmt.Sprintf("installed package %s", packageName))
	variableIDs := make([]deppy.Identifier, 0, len(bundleVariables))
	for _, bundle := range bundleVariables {
		variableIDs = append(variableIDs, bundle.ID)
	}
	return &InstalledPackageVariable{
		SimpleVariable:  input.NewSimpleVariable(id, constraint.Mandatory(), constraint.Dependency(variableIDs...)),
		bundleVariables: bundleVariables,
	}
}
