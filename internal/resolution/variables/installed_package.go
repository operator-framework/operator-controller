package variables

import (
	"fmt"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

var _ deppy.Variable = &InstalledPackageVariable{}

type InstalledPackageVariable struct {
	*input.SimpleVariable
	bundles []*catalogmetadata.Bundle
}

func (r *InstalledPackageVariable) Bundles() []*catalogmetadata.Bundle {
	return r.bundles
}

func NewInstalledPackageVariable(packageName string, bundles []*catalogmetadata.Bundle) *InstalledPackageVariable {
	id := deppy.IdentifierFromString(fmt.Sprintf("installed package %s", packageName))
	variableIDs := make([]deppy.Identifier, 0, len(bundles))
	for _, bundle := range bundles {
		variableIDs = append(variableIDs, BundleVariableID(bundle))
	}
	return &InstalledPackageVariable{
		SimpleVariable: input.NewSimpleVariable(id, constraint.Mandatory(), constraint.Dependency(variableIDs...)),
		bundles:        bundles,
	}
}
