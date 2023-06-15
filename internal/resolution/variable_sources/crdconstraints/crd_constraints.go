package crdconstraints

import (
	"context"
	"fmt"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/bundlesanddependencies"
	olmentity "github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity"
)

type BundleUniquenessVariable struct {
	*input.SimpleVariable
}

// NewBundleUniquenessVariable creates a new variable that instructs the resolver to choose at most a single bundle
// from the input 'atMostID'. Examples:
// 1. restrict the solution to at most a single bundle per package
// 2. restrict the solution to at most a single bundler per provided gvk
// this guarantees that no two operators provide the same gvk and no two version of the same operator are running at the same time
func NewBundleUniquenessVariable(id deppy.Identifier, atMostIDs ...deppy.Identifier) *BundleUniquenessVariable {
	return &BundleUniquenessVariable{
		SimpleVariable: input.NewSimpleVariable(id, constraint.AtMost(1, atMostIDs...)),
	}
}

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
	gvkToBundleMap := map[string]map[deppy.Identifier]struct{}{}
	for _, variable := range variables {
		switch v := variable.(type) {
		case *bundlesanddependencies.BundleVariable:
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

				// get bundleID gvks and update map
				exportedGVKs, err := bundleEntity.ProvidedGVKs()
				if err != nil {
					return nil, fmt.Errorf("error creating global constraints: %w", err)
				}
				for i := 0; i < len(exportedGVKs); i++ {
					gvk := exportedGVKs[i].String()
					if _, ok := gvkToBundleMap[gvk]; !ok {
						gvkToBundleMap[gvk] = map[deppy.Identifier]struct{}{}
					}
					gvkToBundleMap[gvk][bundleEntity.ID] = struct{}{}
				}
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
		variables = append(variables, NewBundleUniquenessVariable(varID, bundleIDs...))
	}

	for gvk, bundleIDMap := range gvkToBundleMap {
		var bundleIDs []deppy.Identifier
		for bundleID := range bundleIDMap {
			bundleIDs = append(bundleIDs, bundleID)
		}
		varID := deppy.IdentifierFromString(fmt.Sprintf("%s gvk uniqueness", gvk))
		variables = append(variables, NewBundleUniquenessVariable(varID, bundleIDs...))
	}

	return variables, nil
}
