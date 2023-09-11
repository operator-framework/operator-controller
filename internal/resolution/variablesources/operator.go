package variablesources

import (
	"context"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	catalogclient "github.com/operator-framework/operator-controller/internal/catalogmetadata/client"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ input.VariableSource = &OperatorVariableSource{}

type OperatorVariableSource struct {
	client              client.Client
	catalog             *catalogclient.Client
	inputVariableSource input.VariableSource
}

func NewOperatorVariableSource(cl client.Client, catalog *catalogclient.Client, inputVariableSource input.VariableSource) *OperatorVariableSource {
	return &OperatorVariableSource{
		client:              cl,
		catalog:             catalog,
		inputVariableSource: inputVariableSource,
	}
}

func (o *OperatorVariableSource) GetVariables(ctx context.Context, _ input.EntitySource) ([]deppy.Variable, error) {
	variableSources := SliceVariableSource{}
	if o.inputVariableSource != nil {
		variableSources = append(variableSources, o.inputVariableSource)
	}

	operatorList := operatorsv1alpha1.OperatorList{}
	if err := o.client.List(ctx, &operatorList); err != nil {
		return nil, err
	}

	// build required package variable sources
	for _, operator := range operatorList.Items {
		rps, err := NewRequiredPackageVariableSource(
			o.catalog,
			operator.Spec.PackageName,
			InVersionRange(operator.Spec.Version),
			InChannel(operator.Spec.Channel),
		)
		if err != nil {
			return nil, err
		}
		variableSources = append(variableSources, rps)
	}

	return variableSources.GetVariables(ctx, nil)
}
