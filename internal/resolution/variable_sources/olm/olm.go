package olm

import (
	"context"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"

	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/bundles_and_dependencies"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/crd_constraints"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/required_package"
)

var _ input.VariableSource = &OLMVariableSource{}

type OLMVariableSource struct {
	packageNames []string
}

func NewOLMVariableSource(packageNames ...string) *OLMVariableSource {
	return &OLMVariableSource{
		packageNames: packageNames,
	}
}

func (o *OLMVariableSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
	var inputVariableSources []input.VariableSource

	// build required package variable sources
	for _, packageName := range o.packageNames {
		inputVariableSources = append(inputVariableSources, required_package.NewRequiredPackage(packageName))
	}

	// build variable source pipeline
	variableSource := crd_constraints.NewCRDUniquenessConstraintsVariableSource(bundles_and_dependencies.NewBundlesAndDepsVariableSource(inputVariableSources...))
	return variableSource.GetVariables(ctx, entitySource)
}
