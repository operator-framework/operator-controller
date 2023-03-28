package olm

import (
	"context"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/bundles_and_dependencies"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/crd_constraints"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/required_package"
)

var _ input.VariableSource = &OLMVariableSource{}

type OLMVariableSource struct {
	operators []operatorsv1alpha1.Operator
}

func NewOLMVariableSource(operators ...operatorsv1alpha1.Operator) *OLMVariableSource {
	return &OLMVariableSource{
		operators: operators,
	}
}

func (o *OLMVariableSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
	var inputVariableSources []input.VariableSource

	// build required package variable sources
	for _, operator := range o.operators {
		rps, err := o.requiredPackageFromOperator(&operator)
		if err != nil {
			return nil, err
		}
		inputVariableSources = append(inputVariableSources, rps)
	}

	// build variable source pipeline
	variableSource := crd_constraints.NewCRDUniquenessConstraintsVariableSource(bundles_and_dependencies.NewBundlesAndDepsVariableSource(inputVariableSources...))
	return variableSource.GetVariables(ctx, entitySource)
}

func (o *OLMVariableSource) requiredPackageFromOperator(operator *operatorsv1alpha1.Operator) (*required_package.RequiredPackageVariableSource, error) {
	var opts []required_package.RequiredPackageOption
	if operator.Spec.Version != "" {
		opts = append(opts, required_package.InVersionRange(operator.Spec.Version))
	}
	return required_package.NewRequiredPackage(operator.Spec.PackageName, opts...)
}
