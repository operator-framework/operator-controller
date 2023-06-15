package olm

import (
	"context"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/bundles_and_dependencies"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/crd_constraints"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/required_package"
)

var _ input.VariableSource = &VariableSource{}

type VariableSource struct {
	client client.Client
}

func NewOLMVariableSource(cl client.Client) *VariableSource {
	return &VariableSource{
		client: cl,
	}
}

func (o *VariableSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
	operatorList := operatorsv1alpha1.OperatorList{}
	if err := o.client.List(ctx, &operatorList); err != nil {
		return nil, err
	}

	// build required package variable sources
	inputVariableSources := make([]input.VariableSource, 0, len(operatorList.Items))
	for _, operator := range operatorList.Items {
		rps, err := required_package.NewRequiredPackage(
			operator.Spec.PackageName,
			required_package.InVersionRange(operator.Spec.Version),
			required_package.InChannel(operator.Spec.Channel),
		)
		if err != nil {
			return nil, err
		}
		inputVariableSources = append(inputVariableSources, rps)
	}

	// build variable source pipeline
	variableSource := crd_constraints.NewCRDUniquenessConstraintsVariableSource(bundles_and_dependencies.NewBundlesAndDepsVariableSource(inputVariableSources...))
	return variableSource.GetVariables(ctx, entitySource)
}
