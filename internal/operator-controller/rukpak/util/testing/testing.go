package testing

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

type CSVOption func(version *v1alpha1.ClusterServiceVersion)

//nolint:unparam
func WithName(name string) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Name = name
	}
}

func WithStrategyDeploymentSpecs(strategyDeploymentSpecs ...v1alpha1.StrategyDeploymentSpec) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs = strategyDeploymentSpecs
	}
}

func WithAnnotations(annotations map[string]string) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Annotations = annotations
	}
}

func WithPermissions(permissions ...v1alpha1.StrategyDeploymentPermissions) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.InstallStrategy.StrategySpec.Permissions = permissions
	}
}

func WithClusterPermissions(permissions ...v1alpha1.StrategyDeploymentPermissions) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.InstallStrategy.StrategySpec.ClusterPermissions = permissions
	}
}

func WithOwnedCRDs(crdDesc ...v1alpha1.CRDDescription) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.CustomResourceDefinitions.Owned = crdDesc
	}
}

func WithInstallModeSupportFor(installModeType ...v1alpha1.InstallModeType) CSVOption {
	var installModes = []v1alpha1.InstallModeType{
		v1alpha1.InstallModeTypeAllNamespaces,
		v1alpha1.InstallModeTypeSingleNamespace,
		v1alpha1.InstallModeTypeMultiNamespace,
		v1alpha1.InstallModeTypeOwnNamespace,
	}
	return func(csv *v1alpha1.ClusterServiceVersion) {
		supportedInstallModes := sets.New(installModeType...)
		csvInstallModes := make([]v1alpha1.InstallMode, 0, len(installModeType))
		for _, t := range installModes {
			csvInstallModes = append(csvInstallModes, v1alpha1.InstallMode{
				Type:      t,
				Supported: supportedInstallModes.Has(t),
			})
		}
		csv.Spec.InstallModes = csvInstallModes
	}
}

func WithWebhookDefinitions(webhookDefinitions ...v1alpha1.WebhookDescription) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.WebhookDefinitions = webhookDefinitions
	}
}

func WithOwnedAPIServiceDescriptions(ownedAPIServiceDescriptions ...v1alpha1.APIServiceDescription) CSVOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.APIServiceDefinitions.Owned = ownedAPIServiceDescriptions
	}
}

func MakeCSV(opts ...CSVOption) v1alpha1.ClusterServiceVersion {
	csv := v1alpha1.ClusterServiceVersion{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha1.SchemeGroupVersion.String(),
			Kind:       "ClusterServiceVersion",
		},
	}
	for _, opt := range opts {
		opt(&csv)
	}
	return csv
}

type FakeCertProvider struct {
	InjectCABundleFn    func(obj client.Object, cfg render.CertificateProvisionerConfig) error
	AdditionalObjectsFn func(cfg render.CertificateProvisionerConfig) ([]unstructured.Unstructured, error)
	GetCertSecretInfoFn func(cfg render.CertificateProvisionerConfig) render.CertSecretInfo
}

func (f FakeCertProvider) InjectCABundle(obj client.Object, cfg render.CertificateProvisionerConfig) error {
	return f.InjectCABundleFn(obj, cfg)
}

func (f FakeCertProvider) AdditionalObjects(cfg render.CertificateProvisionerConfig) ([]unstructured.Unstructured, error) {
	return f.AdditionalObjectsFn(cfg)
}

func (f FakeCertProvider) GetCertSecretInfo(cfg render.CertificateProvisionerConfig) render.CertSecretInfo {
	return f.GetCertSecretInfoFn(cfg)
}

type FakeBundleSource func() (bundle.RegistryV1, error)

func (f FakeBundleSource) GetBundle() (bundle.RegistryV1, error) {
	return f()
}

func ToUnstructuredT(t *testing.T, obj client.Object) *unstructured.Unstructured {
	u, err := util.ToUnstructured(obj)
	require.NoError(t, err)
	return u
}
