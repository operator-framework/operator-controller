package applier

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"helm.sh/helm/v3/pkg/chart"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
)

type RegistryV1HelmChartProvider struct {
	BundleRenderer              render.BundleRenderer
	CertificateProvider         render.CertificateProvider
	IsWebhookSupportEnabled     bool
	IsSingleOwnNamespaceEnabled bool
}

func (r *RegistryV1HelmChartProvider) Get(bundle source.BundleSource, ext *ocv1.ClusterExtension) (*chart.Chart, error) {
	rv1, err := bundle.GetBundle()
	if err != nil {
		return nil, err
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

	installModes := sets.New(rv1.CSV.Spec.InstallModes...)
	if !r.IsSingleOwnNamespaceEnabled && !installModes.Has(v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true}) {
		return nil, fmt.Errorf("unsupported bundle: bundle does not support AllNamespaces install mode")
	}

	if !installModes.HasAny(
		v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeAllNamespaces, Supported: true},
		v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeSingleNamespace, Supported: true},
		v1alpha1.InstallMode{Type: v1alpha1.InstallModeTypeOwnNamespace, Supported: true},
	) {
		return nil, fmt.Errorf("unsupported bundle: bundle must support at least one of [AllNamespaces SingleNamespace OwnNamespace] install modes")
	}

	opts := []render.Option{
		render.WithCertificateProvider(r.CertificateProvider),
	}

	// TODO: in a follow up PR we'll split this into two components:
	//   1. takes a bundle + cluster extension => manifests
	//   2. takes a bundle + cluster extension => chart (which will use the component in 1. under the hood)
	// GetWatchNamespace will move under the component in 1. and also be reused by the component that
	// takes bundle + cluster extension => revision
	watchNamespace, err := GetWatchNamespace(ext)
	if err != nil {
		return nil, err
	}

	if watchNamespace != "" {
		opts = append(opts, render.WithTargetNamespaces(watchNamespace))
	}

	objs, err := r.BundleRenderer.Render(rv1, ext.Spec.Namespace, opts...)

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
