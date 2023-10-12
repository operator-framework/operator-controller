package variablesources

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/deppy/pkg/deppy"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

func MakeBundleUniquenessVariables(bundleVariables []*olmvariables.BundleVariable) ([]*olmvariables.BundleUniquenessVariable, error) {
	result := []*olmvariables.BundleUniquenessVariable{}

	bundleIDs := sets.Set[deppy.Identifier]{}
	packageOrder := []string{}
	bundleOrder := map[string][]deppy.Identifier{}
	for _, bundleVariable := range bundleVariables {
		bundles := []*catalogmetadata.Bundle{bundleVariable.Bundle()}
		bundles = append(bundles, bundleVariable.Dependencies()...)
		for _, bundle := range bundles {
			id := olmvariables.BundleVariableID(bundle)
			// get bundleID package and update map
			packageName := bundle.Package

			if _, ok := bundleOrder[packageName]; !ok {
				packageOrder = append(packageOrder, packageName)
			}

			if !bundleIDs.Has(id) {
				bundleIDs.Insert(id)
				bundleOrder[packageName] = append(bundleOrder[packageName], id)
			}
		}
	}

	// create global constraint variables
	for _, packageName := range packageOrder {
		varID := deppy.IdentifierFromString(fmt.Sprintf("%s package uniqueness", packageName))
		result = append(result, olmvariables.NewBundleUniquenessVariable(varID, bundleOrder[packageName]...))
	}

	return result, nil
}
