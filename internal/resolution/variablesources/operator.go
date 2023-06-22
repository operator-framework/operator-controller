package variablesources

import (
	"context"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

var _ input.VariableSource = &OperatorVariableSource{}

type OperatorVariableSource struct {
	client client.Client
}

func NewOperatorVariableSource(cl client.Client) *OperatorVariableSource {
	return &OperatorVariableSource{
		client: cl,
	}
}

func (o *OperatorVariableSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
	operatorList := operatorsv1alpha1.OperatorList{}
	if err := o.client.List(ctx, &operatorList); err != nil {
		return nil, err
	}

	// build required package variable sources
	inputVariableSources := make([]input.VariableSource, 0, len(operatorList.Items))
	for _, operator := range operatorList.Items {
		rps, err := NewRequiredPackageVariableSource(
			operator.Spec.PackageName,
			InVersionRange(operator.Spec.Version),
			InChannel(operator.Spec.Channel),
		)
		if err != nil {
			return nil, err
		}
		inputVariableSources = append(inputVariableSources, rps)
	}

	// build variable source pipeline
	variableSource := NewCRDUniquenessConstraintsVariableSource(NewBundlesAndDepsVariableSource(inputVariableSources...))
	return variableSource.GetVariables(ctx, entitySource)
}
