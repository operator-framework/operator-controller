package applier

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"

	"helm.sh/helm/v3/pkg/chart"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
)

// ManifestProvider returns the manifests that should be applied by OLM given a bundle and its associated ClusterExtension
type ManifestProvider interface {
	// Get returns a set of resource manifests in bundle that take into account the configuration in ext
	Get(bundle fs.FS, ext *ocv1.ClusterExtension) ([]client.Object, error)
}

// RegistryV1ManifestProvider generates the manifests that should be installed for a registry+v1 bundle
// given the user specified configuration given by the ClusterExtension API surface
type RegistryV1ManifestProvider struct {
	BundleRenderer              render.BundleRenderer
	CertificateProvider         render.CertificateProvider
	IsWebhookSupportEnabled     bool
	IsSingleOwnNamespaceEnabled bool
}

func (r *RegistryV1ManifestProvider) Get(bundleFS fs.FS, ext *ocv1.ClusterExtension) ([]client.Object, error) {
	rv1, err := source.FromFS(bundleFS).GetBundle()
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

	if r.IsSingleOwnNamespaceEnabled {
		bundleConfigBytes := extensionConfigBytes(ext)
		// treat no config as empty to properly validate the configuration
		// e.g. ensure that validation catches missing required fields
		if bundleConfigBytes == nil {
			bundleConfigBytes = []byte(`{}`)
		}
		bundleConfig, err := bundle.UnmarshalConfig(bundleConfigBytes, rv1, ext.Spec.Namespace)
		if err != nil {
			return nil, fmt.Errorf("invalid bundle configuration: %w", err)
		}

		if bundleConfig != nil && bundleConfig.WatchNamespace != nil {
			opts = append(opts, render.WithTargetNamespaces(*bundleConfig.WatchNamespace))
		}
	}

	return r.BundleRenderer.Render(rv1, ext.Spec.Namespace, opts...)
}

// RegistryV1HelmChartProvider creates a Helm-Chart from a registry+v1 bundle and its associated ClusterExtension
type RegistryV1HelmChartProvider struct {
	ManifestProvider ManifestProvider
}

func (r *RegistryV1HelmChartProvider) Get(bundleFS fs.FS, ext *ocv1.ClusterExtension) (*chart.Chart, error) {
	objs, err := r.ManifestProvider.Get(bundleFS, ext)
	if err != nil {
		return nil, err
	}

	chrt := &chart.Chart{Metadata: &chart.Metadata{}}
	// The need to get the underlying bundle in order to extract its annotations
	// will go away once with have a bundle interface that can surface the annotations independently of the
	// underlying bundle format...
	rv1, err := source.FromFS(bundleFS).GetBundle()
	if err != nil {
		return nil, err
	}
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

// ExtensionConfigBytes returns the ClusterExtension configuration input by the user
// through .spec.config as a byte slice.
func extensionConfigBytes(ext *ocv1.ClusterExtension) []byte {
	if ext.Spec.Config != nil {
		switch ext.Spec.Config.ConfigType {
		case ocv1.ClusterExtensionConfigTypeInline:
			if ext.Spec.Config.Inline != nil {
				return ext.Spec.Config.Inline.Raw
			}
		}
	}
	return nil
}
