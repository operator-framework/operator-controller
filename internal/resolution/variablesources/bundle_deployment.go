package variablesources

import (
	"context"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	LabelPackageName   = "operators.operatorframework.io/package-name"
	LabelBundleName    = "operators.operatorframework.io/bundle-name"
	LabelBundleVersion = "operators.operatorframework.io/bundle-version"
)

var _ input.VariableSource = &BundleDeploymentVariableSource{}

type BundleDeploymentVariableSource struct {
	client              client.Client
	catalogClient       BundleProvider
	inputVariableSource input.VariableSource
}

func NewBundleDeploymentVariableSource(cl client.Client, catalogClient BundleProvider, inputVariableSource input.VariableSource) *BundleDeploymentVariableSource {
	return &BundleDeploymentVariableSource{
		client:              cl,
		catalogClient:       catalogClient,
		inputVariableSource: inputVariableSource,
	}
}

func (o *BundleDeploymentVariableSource) GetVariables(ctx context.Context) ([]deppy.Variable, error) {
	variableSources := SliceVariableSource{}
	if o.inputVariableSource != nil {
		variableSources = append(variableSources, o.inputVariableSource)
	}

	bundleDeployments := rukpakv1alpha1.BundleDeploymentList{}
	if err := o.client.List(ctx, &bundleDeployments); err != nil {
		return nil, err
	}

	for _, bundleDeployment := range bundleDeployments.Items {
		pkgName := bundleDeployment.Labels[LabelPackageName]
		bundleName := bundleDeployment.Labels[LabelBundleName]
		bundleVersion := bundleDeployment.Labels[LabelBundleVersion]
		if pkgName == "" || bundleName == "" || bundleVersion == "" {
			continue
		}
		ips := NewInstalledPackageVariableSource(o.catalogClient, pkgName, bundleName, bundleVersion)
		variableSources = append(variableSources, ips)
	}

	return variableSources.GetVariables(ctx)
}
