package variablesources

import (
	"context"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

var _ input.VariableSource = &BundleDeploymentVariableSource{}

type BundleDeploymentVariableSource struct {
	operators           []operatorsv1alpha1.Operator
	bundleDeployments   []rukpakv1alpha1.BundleDeployment
	allBundles          []*catalogmetadata.Bundle
	inputVariableSource input.VariableSource
}

func NewBundleDeploymentVariableSource(operators []operatorsv1alpha1.Operator, bundleDeployments []rukpakv1alpha1.BundleDeployment, allBundles []*catalogmetadata.Bundle, inputVariableSource input.VariableSource) *BundleDeploymentVariableSource {
	return &BundleDeploymentVariableSource{
		operators:           operators,
		bundleDeployments:   bundleDeployments,
		allBundles:          allBundles,
		inputVariableSource: inputVariableSource,
	}
}

func (o *BundleDeploymentVariableSource) GetVariables(ctx context.Context) ([]deppy.Variable, error) {
	variableSources := SliceVariableSource{}
	if o.inputVariableSource != nil {
		variableSources = append(variableSources, o.inputVariableSource)
	}

	variables, err := variableSources.GetVariables(ctx)
	if err != nil {
		return nil, err
	}

	installedPackages, err := MakeInstalledPackageVariables(o.allBundles, o.operators, o.bundleDeployments)
	if err != nil {
		return nil, err
	}

	for _, v := range installedPackages {
		variables = append(variables, v)
	}
	return variables, nil
}
