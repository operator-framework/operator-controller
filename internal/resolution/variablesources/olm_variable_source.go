package variablesources

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/deppy/pkg/deppy"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

type OLMVariableSource struct {
	client        client.Client
	catalogClient BundleProvider
}

func NewOLMVariableSource(cl client.Client, catalogClient BundleProvider) *OLMVariableSource {
	return &OLMVariableSource{
		client:        cl,
		catalogClient: catalogClient,
	}
}

func (o *OLMVariableSource) GetVariables(ctx context.Context) ([]deppy.Variable, error) {
	operatorList := operatorsv1alpha1.OperatorList{}
	if err := o.client.List(ctx, &operatorList); err != nil {
		return nil, err
	}

	bundleDeploymentList := rukpakv1alpha1.BundleDeploymentList{}
	if err := o.client.List(ctx, &bundleDeploymentList); err != nil {
		return nil, err
	}

	allBundles, err := o.catalogClient.Bundles(ctx)
	if err != nil {
		return nil, err
	}

	requiredPackages, err := MakeRequiredPackageVariables(allBundles, operatorList.Items)
	if err != nil {
		return nil, err
	}

	installedPackages, err := MakeInstalledPackageVariables(allBundles, bundleDeploymentList.Items)
	if err != nil {
		return nil, err
	}

	bundles, err := MakeBundleVariables(allBundles, requiredPackages, installedPackages)
	if err != nil {
		return nil, err
	}

	bundleUniqueness, err := MakeBundleUniquenessVariables(bundles)
	if err != nil {
		return nil, err
	}

	result := []deppy.Variable{}
	for _, v := range requiredPackages {
		result = append(result, v)
	}
	for _, v := range installedPackages {
		result = append(result, v)
	}
	for _, v := range bundles {
		result = append(result, v)
	}
	for _, v := range bundleUniqueness {
		result = append(result, v)
	}
	return result, nil
}
