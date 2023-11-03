package variablesources

import (
	"context"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
)

var _ input.VariableSource = &OperatorVariableSource{}

type OperatorVariableSource struct {
	operators           []operatorsv1alpha1.Operator
	allBundles          []*catalogmetadata.Bundle
	inputVariableSource input.VariableSource
}

func NewOperatorVariableSource(operators []operatorsv1alpha1.Operator, allBundles []*catalogmetadata.Bundle, inputVariableSource input.VariableSource) *OperatorVariableSource {
	return &OperatorVariableSource{
		operators:           operators,
		allBundles:          allBundles,
		inputVariableSource: inputVariableSource,
	}
}

func (o *OperatorVariableSource) GetVariables(ctx context.Context) ([]deppy.Variable, error) {
	variableSources := SliceVariableSource{}
	if o.inputVariableSource != nil {
		variableSources = append(variableSources, o.inputVariableSource)
	}

	// build required package variable sources
	for _, operator := range o.operators {
		rps, err := NewRequiredPackageVariableSource(
			o.allBundles,
			operator.Spec.PackageName,
			InVersionRange(operator.Spec.Version),
			InChannel(operator.Spec.Channel),
		)
		if err != nil {
			return nil, err
		}
		variableSources = append(variableSources, rps)
	}

	return variableSources.GetVariables(ctx)
}
