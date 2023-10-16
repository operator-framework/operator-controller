package variablesources

import (
	"context"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	processed := sets.Set[string]{}
	for _, bundleDeployment := range bundleDeployments.Items {
		sourceImage := bundleDeployment.Spec.Template.Spec.Source.Image
		if sourceImage != nil && sourceImage.Ref != "" {
			if processed.Has(sourceImage.Ref) {
				continue
			}
			processed.Insert(sourceImage.Ref)
			ips, err := NewInstalledPackageVariableSource(o.catalogClient, bundleDeployment.Spec.Template.Spec.Source.Image.Ref)
			if err != nil {
				return nil, err
			}
			variableSources = append(variableSources, ips)
		}
	}

	return variableSources.GetVariables(ctx)
}
