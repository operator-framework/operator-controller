package variablesources

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

// MakeBundleUniquenessVariables produces variables that constrain
// the solution to at most 1 bundle per package.
// These variables guarantee that no two versions of
// the same package are running at the same time.
func MakeBundleUniquenessVariables(bundleVariables []*olmvariables.BundleVariable) []*olmvariables.BundleUniquenessVariable {
	result := []*olmvariables.BundleUniquenessVariable{}

	bundleIDs := sets.Set[deppy.Identifier]{}
	packageOrder := []string{}
	bundleOrder := map[string][]deppy.Identifier{}
	for _, bundleVariable := range bundleVariables {
		bundles := make([]*catalogmetadata.Bundle, 0, 1+len(bundleVariable.Dependencies()))
		bundles = append(bundles, bundleVariable.Bundle())
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

	return result
}

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

	bundleVariables := []*olmvariables.BundleVariable{}
	for _, variable := range variables {
		switch v := variable.(type) {
		case *olmvariables.BundleVariable:
			bundleVariables = append(bundleVariables, v)
		}
	}

	bundleUniqueness := MakeBundleUniquenessVariables(bundleVariables)
	for _, v := range bundleUniqueness {
		variables = append(variables, v)
	}
	return variables, nil
}
