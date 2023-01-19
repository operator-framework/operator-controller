package variable_sources

import (
	"context"
	"fmt"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/pkg/api"
)

type GlobalConstraintVariable struct {
	*input.SimpleVariable
}

func NewGlobalConstraintVariable(id deppy.Identifier, atMostIDs ...deppy.Identifier) *GlobalConstraintVariable {
	return &GlobalConstraintVariable{
		SimpleVariable: input.NewSimpleVariable(id, constraint.AtMost(1, atMostIDs...)),
	}
}

var _ input.VariableSource = &GlobalConstraintVariableSource{}

type GlobalConstraintVariableSource struct {
	inputVariableSource input.VariableSource
}

func NewGlobalConstraintVariableSource(inputVariableSource input.VariableSource) *GlobalConstraintVariableSource {
	return &GlobalConstraintVariableSource{
		inputVariableSource: inputVariableSource,
	}
}

func (g *GlobalConstraintVariableSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
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
		case *BundleVariable:
			bundleEntities := []*BundleEntity{v.BundleEntity()}
			bundleEntities = append(bundleEntities, v.Dependencies()...)
			for _, bundleEntity := range bundleEntities {
				// get bundleID package and update map
				packageName, err := bundleEntity.PackageName()
				if err != nil {
					return nil, fmt.Errorf("error creating global constraints: %s", err)
				}

				if _, ok := pkgToBundleMap[packageName]; !ok {
					pkgToBundleMap[packageName] = map[deppy.Identifier]struct{}{}
				}
				pkgToBundleMap[packageName][bundleEntity.ID] = struct{}{}

				// get bundleID gvks and update map
				exportedGVKs, err := bundleEntity.ProvidedGVKs()
				if err != nil {
					return nil, fmt.Errorf("error creating global constraints: %s", err)
				}
				for i := 0; i < len(exportedGVKs); i++ {
					gvk := gvkToString(&exportedGVKs[i])
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
		for bundleID, _ := range bundleIDMap {
			bundleIDs = append(bundleIDs, bundleID)
		}
		varID := deppy.IdentifierFromString(fmt.Sprintf("%s package uniqueness", packageName))
		variables = append(variables, NewGlobalConstraintVariable(varID, bundleIDs...))
	}

	for gvk, bundleIDMap := range gvkToBundleMap {
		var bundleIDs []deppy.Identifier
		for bundleID, _ := range bundleIDMap {
			bundleIDs = append(bundleIDs, bundleID)
		}
		varID := deppy.IdentifierFromString(fmt.Sprintf("%s gvk uniqueness", gvk))
		variables = append(variables, NewGlobalConstraintVariable(varID, bundleIDs...))
	}

	return variables, nil
}

func gvkToString(gvk *api.GroupVersionKind) string {
	return fmt.Sprintf(`group:"%s" version:"%s" kind:"%s"`, gvk.Group, gvk.Version, gvk.Kind)
}
