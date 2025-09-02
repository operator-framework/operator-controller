package convert

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"helm.sh/helm/v3/pkg/chart"

	bundlepkg "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
)

type BundleToHelmChartConverter struct {
	BundleRenderer          render.BundleRenderer
	CertificateProvider     render.CertificateProvider
	IsWebhookSupportEnabled bool
}

func (r *BundleToHelmChartConverter) ToHelmChart(bundle source.BundleSource, installNamespace string, config map[string]interface{}) (*chart.Chart, error) {
	rv1, err := bundle.GetBundle()
	if err != nil {
		return nil, err
	}

	opts := []render.Option{
		render.WithCertificateProvider(r.CertificateProvider),
	}
	if config != nil {
		if watchNs, ok := config[bundlepkg.BundleConfigWatchNamespaceKey].(string); ok {
			opts = append(opts, render.WithTargetNamespaces(watchNs))
		}
	}

	if len(rv1.CSV.Spec.APIServiceDefinitions.Owned) > 0 {
		return nil, fmt.Errorf("unsupported bundle: apiServiceDefintions are not supported")
	}

	if len(rv1.CSV.Spec.WebhookDefinitions) > 0 {
		if !r.IsWebhookSupportEnabled {
			return nil, fmt.Errorf("unsupported bundle: webhookDefinitions are not supported")
		} else if r.CertificateProvider == nil {
			return nil, fmt.Errorf("unsupported bundle: webhookDefinitions are not supported: certificate provider is nil")
		}
	}

	if r.CertificateProvider == nil && len(rv1.CSV.Spec.WebhookDefinitions) > 0 {
		return nil, fmt.Errorf("unsupported bundle: webhookDefinitions are not supported")
	}

	objs, err := r.BundleRenderer.Render(rv1, installNamespace, opts...)

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
		name := fmt.Sprintf("object-%x.json", hash[0:8])

		// Some registry+v1 manifests may actually contain Go Template strings
		// that are meant to survive and actually persist into etcd (e.g. to be
		// used as a templated configuration for another component). In order to
		// avoid applying templating logic to registry+v1's static manifests, we
		// create the manifests as Files, and then template those files via simple
		// Templates.
		chrt.Files = append(chrt.Files, &chart.File{
			Name: name,
			Data: jsonData,
		})
		chrt.Templates = append(chrt.Templates, &chart.File{
			Name: name,
			Data: []byte(fmt.Sprintf(`{{.Files.Get "%s"}}`, name)),
		})
	}

	return chrt, nil
}
