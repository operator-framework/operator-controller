package variablesources

import (
	"context"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	catalogclient "github.com/operator-framework/operator-controller/internal/catalogmetadata/client"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ input.VariableSource = &BundleDeploymentVariableSource{}

type BundleDeploymentVariableSource struct {
	client              client.Client
	catalog             *catalogclient.Client
	inputVariableSource input.VariableSource
}

func NewBundleDeploymentVariableSource(cl client.Client, catalog *catalogclient.Client, inputVariableSource input.VariableSource) *BundleDeploymentVariableSource {
	return &BundleDeploymentVariableSource{
		client:              cl,
		catalog:             catalog,
		inputVariableSource: inputVariableSource,
	}
}

func (o *BundleDeploymentVariableSource) GetVariables(ctx context.Context, _ input.EntitySource) ([]deppy.Variable, error) {
	variableSources := SliceVariableSource{}
	if o.inputVariableSource != nil {
		variableSources = append(variableSources, o.inputVariableSource)
	}

	bundleDeployments := rukpakv1alpha1.BundleDeploymentList{}
	if err := o.client.List(ctx, &bundleDeployments); err != nil {
		return nil, err
	}

	processed := map[string]struct{}{}
	for _, bundleDeployment := range bundleDeployments.Items {
		sourceImage := bundleDeployment.Spec.Template.Spec.Source.Image
		if sourceImage != nil && sourceImage.Ref != "" {
			if _, ok := processed[sourceImage.Ref]; ok {
				continue
			}
			processed[sourceImage.Ref] = struct{}{}
			ips, err := NewInstalledPackageVariableSource(o.catalog, bundleDeployment.Spec.Template.Spec.Source.Image.Ref)
			if err != nil {
				return nil, err
			}
			variableSources = append(variableSources, ips)
		}
	}

	return variableSources.GetVariables(ctx, nil)
}
