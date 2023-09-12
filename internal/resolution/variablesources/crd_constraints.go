package variablesources

import (
	"context"
	"fmt"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

var _ input.VariableSource = &CRDUniquenessConstraintsVariableSource{}

// CRDUniquenessConstraintsVariableSource produces variables that constraint the solution to
// 1. at most 1 bundle per package
// 2. at most 1 bundle per gvk (provided by the bundle)
// these variables guarantee that no two operators provide the same gvk and no two version of
// the same operator are running at the same time.
// This variable source does not itself reach out to its entitySource. It produces its variables
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

func (g *CRDUniquenessConstraintsVariableSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
	variables, err := g.inputVariableSource.GetVariables(ctx, entitySource)
	if err != nil {
		return nil, err
	}

	// todo(perdasilva): better handle cases where a provided gvk is not found
	//                   not all packages will necessarily export a CRD

	pkgToBundleMap := map[string]map[deppy.Identifier]struct{}{}
	for _, variable := range variables {
		switch v := variable.(type) {
		case *olmvariables.BundleVariable:
			bundleEntities := []*olmentity.BundleEntity{v.BundleEntity()}
			bundleEntities = append(bundleEntities, v.Dependencies()...)
			for _, bundleEntity := range bundleEntities {
				// get bundleID package and update map
				packageName, err := bundleEntity.PackageName()
				if err != nil {
					return nil, fmt.Errorf("error creating global constraints: %w", err)
				}

				if _, ok := pkgToBundleMap[packageName]; !ok {
					pkgToBundleMap[packageName] = map[deppy.Identifier]struct{}{}
				}
				pkgToBundleMap[packageName][bundleEntity.ID] = struct{}{}
			}
		}
	}

	// create global constraint variables
	for packageName, bundleIDMap := range pkgToBundleMap {
		var bundleIDs []deppy.Identifier
		for bundleID := range bundleIDMap {
			bundleIDs = append(bundleIDs, bundleID)
		}
		varID := deppy.IdentifierFromString(fmt.Sprintf("%s package uniqueness", packageName))
		variables = append(variables, olmvariables.NewBundleUniquenessVariable(varID, bundleIDs...))
	}

	return variables, nil
}
