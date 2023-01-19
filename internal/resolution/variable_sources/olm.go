package variable_sources

import (
	"context"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
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
		inputVariableSources = append(inputVariableSources, NewRequiredPackage(packageName))
	}

	// build variable source pipeline
	variableSource := NewGlobalConstraintVariableSource(NewBundlesAndDepsVariableSource(inputVariableSources...))
	return variableSource.GetVariables(ctx, entitySource)
}
