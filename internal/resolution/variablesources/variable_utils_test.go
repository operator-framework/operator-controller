package variablesources_test

import (
	"github.com/operator-framework/deppy/pkg/deppy"

	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

func findVariableWithName(vars []*olmvariables.BundleVariable, name string) *olmvariables.BundleVariable {
	for i := 0; i < len(vars); i++ {
		if vars[i].Bundle().Name == name {
			return vars[i]
		}
	}
	return nil
}

func collectVariableIDs[T deppy.Variable](vars []T) []string {
	ids := make([]string, 0, len(vars))
	for _, v := range vars {
		ids = append(ids, v.Identifier().String())
	}
	return ids
}
