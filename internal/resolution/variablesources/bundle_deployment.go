package variablesources

import (
	"context"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

var _ input.VariableSource = &BundleDeploymentVariableSource{}

type BundleDeploymentVariableSource struct {
	bundleDeployments   []rukpakv1alpha1.BundleDeployment
	allBundles          []*catalogmetadata.Bundle
	inputVariableSource input.VariableSource
}

func NewBundleDeploymentVariableSource(bundleDeployments []rukpakv1alpha1.BundleDeployment, allBundles []*catalogmetadata.Bundle, inputVariableSource input.VariableSource) *BundleDeploymentVariableSource {
	return &BundleDeploymentVariableSource{
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

	processed := sets.Set[string]{}
	for _, bundleDeployment := range o.bundleDeployments {
		sourceImage := bundleDeployment.Spec.Template.Spec.Source.Image
		if sourceImage != nil && sourceImage.Ref != "" {
			if processed.Has(sourceImage.Ref) {
				continue
			}
			processed.Insert(sourceImage.Ref)
			ips, err := NewInstalledPackageVariableSource(o.allBundles, bundleDeployment.Spec.Template.Spec.Source.Image.Ref)
			if err != nil {
				return nil, err
			}
			variableSources = append(variableSources, ips)
		}
	}

	return variableSources.GetVariables(ctx)
}
