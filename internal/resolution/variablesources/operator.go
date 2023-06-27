package variablesources

import (
	"context"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ input.VariableSource = &OperatorVariableSource{}

type OperatorVariableSource struct {
	client              client.Client
	inputVariableSource input.VariableSource
}

func NewOperatorVariableSource(cl client.Client, inputVariableSource input.VariableSource) *OperatorVariableSource {
	return &OperatorVariableSource{
		client:              cl,
		inputVariableSource: inputVariableSource,
	}
}

func (o *OperatorVariableSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
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
			operator.Spec.PackageName,
			InVersionRange(operator.Spec.Version),
			InChannel(operator.Spec.Channel),
		)
		if err != nil {
			return nil, err
		}
		variableSources = append(variableSources, rps)
	}

	bundleDeployments := rukpakv1alpha1.BundleDeploymentList{}
	if err := o.client.List(ctx, &bundleDeployments); err != nil {
		return nil, err
	}

	for _, bundleDeployment := range bundleDeployments.Items {
		if _, ok := bundleDeployment.Annotations["operators.operatorframework.io/package"]; !ok {
			continue
		}
		ips, err := NewInstalledPackageVariableSource(&bundleDeployment)
		if err != nil {
			return nil, err
		}
		variableSources = append(variableSources, ips)
	}

	return variableSources.GetVariables(ctx, entitySource)
}
