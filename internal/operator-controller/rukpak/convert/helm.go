package convert

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"helm.sh/helm/v3/pkg/chart"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
)

type BundleToHelmChartConverter struct {
	BundleRenderer      render.BundleRenderer
	CertificateProvider render.CertificateProvider
}

func (r *BundleToHelmChartConverter) ToHelmChart(bundle source.BundleSource, installNamespace string, watchNamespace string) (*chart.Chart, error) {
	rv1, err := bundle.GetBundle()
	if err != nil {
		return nil, err
	}

	if len(rv1.CSV.Spec.APIServiceDefinitions.Owned) > 0 {
		return nil, fmt.Errorf("unsupported bundle: apiServiceDefintions are not supported")
	}

	if r.CertificateProvider == nil && len(rv1.CSV.Spec.WebhookDefinitions) > 0 {
		return nil, fmt.Errorf("unsupported bundle: webhookDefinitions are not supported")
	}

	objs, err := r.BundleRenderer.Render(
		rv1, installNamespace,
		render.WithTargetNamespaces(watchNamespace),
		render.WithCertificateProvider(r.CertificateProvider),
	)

	if err != nil {
		return nil, fmt.Errorf("error rendering bundle: %w", err)
	}

	chrt := &chart.Chart{Metadata: &chart.Metadata{}}
	chrt.Metadata.Annotations = rv1.CSV.GetAnnotations()
	for _, obj := range objs {
		jsonData, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(jsonData)
		chrt.Templates = append(chrt.Templates, &chart.File{
			Name: fmt.Sprintf("object-%x.json", hash[0:8]),
			Data: jsonData,
		})
	}

	return chrt, nil
}
