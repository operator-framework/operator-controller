package variablesources

import (
	"context"
	"fmt"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

var _ input.VariableSource = &CRDUniquenessConstraintsVariableSource{}

// CRDUniquenessConstraintsVariableSource produces variables that constraint the solution to
// 1. at most 1 bundle per package
// 2. at most 1 bundle per gvk (provided by the bundle)
// these variables guarantee that no two operators provide the same gvk and no two version of
// the same operator are running at the same time.
// This variable source does not itself reach out to catalog metadata. It produces its variables
// by searching for BundleVariables that are produced by its 'inputVariableSource' and working out
// which bundles correspond to which package and which gvks are provided by which bundle
type CRDUniquenessConstraintsVariableSource struct {
	inputVariableSource input.VariableSource
}

// NewCRDUniquenessConstraintsVariableSource creates a new instance of the CRDUniquenessConstraintsVariableSource.
// its purpose if to provide variables with constraints that restrict the solutions to bundle sets where
// no two bundles come from the same package and not two bundles provide the same gvk
func NewCRDUniquenessConstraintsVariableSource(inputVariableSource input.VariableSource) *CRDUniquenessConstraintsVariableSource {
	return &CRDUniquenessConstraintsVariableSource{
		inputVariableSource: inputVariableSource,
	}
}

func (g *CRDUniquenessConstraintsVariableSource) GetVariables(ctx context.Context) ([]deppy.Variable, error) {
	variables, err := g.inputVariableSource.GetVariables(ctx)
	if err != nil {
		return nil, err
	}

	// todo(perdasilva): better handle cases where a provided gvk is not found
	//                   not all packages will necessarily export a CRD

	bundleIDs := sets.Set[deppy.Identifier]{}
	packageOrder := []string{}
	bundleOrder := map[string][]deppy.Identifier{}
	for _, variable := range variables {
		switch v := variable.(type) {
		case *olmvariables.BundleVariable:
			bundles := []*catalogmetadata.Bundle{v.Bundle()}
			bundles = append(bundles, v.Dependencies()...)
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
	}

	// create global constraint variables
	for _, packageName := range packageOrder {
		varID := deppy.IdentifierFromString(fmt.Sprintf("%s package uniqueness", packageName))
		variables = append(variables, olmvariables.NewBundleUniquenessVariable(varID, bundleOrder[packageName]...))
	}

	return variables, nil
}
