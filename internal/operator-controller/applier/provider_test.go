package applier_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/clusterserviceversion"
)

func Test_RegistryV1HelmChartProvider_Get_ReturnsBundleSourceFailures(t *testing.T) {
	provider := applier.RegistryV1HelmChartProvider{}
	var failingBundleSource FakeBundleSource = func() (bundle.RegistryV1, error) {
		return bundle.RegistryV1{}, errors.New("some error")
	}
	ext := &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "install-namespace",
		},
	}
	_, err := provider.Get(failingBundleSource, ext)
	require.Error(t, err)
	require.Contains(t, err.Error(), "some error")
}

func Test_RegistryV1HelmChartProvider_Get_ReturnsBundleRendererFailures(t *testing.T) {
	provider := applier.RegistryV1HelmChartProvider{
		BundleRenderer: render.BundleRenderer{
			ResourceGenerators: []render.ResourceGenerator{
				func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
					return nil, errors.New("some error")
				},
			},
		},
	}

	b := source.FromBundle(
		bundle.RegistryV1{
			CSV: clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).Build(),
		},
	)

	ext := &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "install-namespace",
		},
	}
	_, err := provider.Get(b, ext)
	require.Error(t, err)
	require.Contains(t, err.Error(), "some error")
}

func Test_RegistryV1HelmChartProvider_Get_NoAPIServiceDefinitions(t *testing.T) {
	provider := applier.RegistryV1HelmChartProvider{}

	b := source.FromBundle(
		bundle.RegistryV1{
			CSV: clusterserviceversion.Builder().WithOwnedAPIServiceDescriptions(v1alpha1.APIServiceDescription{}).Build(),
		},
	)

	ext := &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "install-namespace",
		},
	}

	_, err := provider.Get(b, ext)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported bundle: apiServiceDefintions are not supported")
}

func Test_RegistryV1HelmChartProvider_Get_SingleOwnNamespace(t *testing.T) {
	t.Run("rejects bundles without AllNamespaces install mode support if SingleOwnNamespace is not enabled", func(t *testing.T) {
		provider := applier.RegistryV1HelmChartProvider{}

		b := source.FromBundle(
			bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeOwnNamespace).Build(),
			},
		)

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		}

		_, err := provider.Get(b, ext)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported bundle: bundle does not support AllNamespaces install mode")
	})
	t.Run("accepts bundles with SingleNamespace install mode support if SingleOwnNamespace is enabled", func(t *testing.T) {
		// TODO: this will be removed in a follow-up PR that will refactor GetWatchNamespace's location
		featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.SingleOwnNamespaceInstallSupport, true)
		provider := applier.RegistryV1HelmChartProvider{
			IsSingleOwnNamespaceEnabled: true,
		}

		b := source.FromBundle(
			bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeSingleNamespace).Build(),
			},
		)

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
				Config: &ocv1.ClusterExtensionConfig{
					ConfigType: ocv1.ClusterExtensionConfigTypeInline,
					Inline: &apiextensionsv1.JSON{
						Raw: []byte(`{"watchNamespace": "some-namespace"}`),
					},
				},
			},
		}

		_, err := provider.Get(b, ext)
		require.NoError(t, err)
	})
	t.Run("accepts bundles with OwnNamespace install mode support if SingleOwnNamespace is enabled", func(t *testing.T) {
		// TODO: this will be removed in a follow-up PR that will refactor GetWatchNamespace's location
		featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.SingleOwnNamespaceInstallSupport, true)
		provider := applier.RegistryV1HelmChartProvider{
			IsSingleOwnNamespaceEnabled: true,
		}

		b := source.FromBundle(
			bundle.RegistryV1{
				CSV: clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeOwnNamespace).Build(),
			},
		)

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		}

		_, err := provider.Get(b, ext)
		require.NoError(t, err)
	})
}

func Test_RegistryV1HelmChartProvider_Get_NoWebhooksWithoutCertProvider(t *testing.T) {
	provider := applier.RegistryV1HelmChartProvider{
		IsWebhookSupportEnabled: true,
	}

	b := source.FromBundle(
		bundle.RegistryV1{
			CSV: clusterserviceversion.Builder().WithWebhookDefinitions(v1alpha1.WebhookDescription{}).Build(),
		},
	)

	ext := &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "install-namespace",
		},
	}

	_, err := provider.Get(b, ext)
	require.Error(t, err)
	require.Contains(t, err.Error(), "webhookDefinitions are not supported")
}

func Test_RegistryV1HelmChartProvider_Get_WebhooksSupportDisabled(t *testing.T) {
	provider := applier.RegistryV1HelmChartProvider{
		IsWebhookSupportEnabled: false,
	}

	b := source.FromBundle(
		bundle.RegistryV1{
			CSV: clusterserviceversion.Builder().WithWebhookDefinitions(v1alpha1.WebhookDescription{}).Build(),
		},
	)

	ext := &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "install-namespace",
		},
	}

	_, err := provider.Get(b, ext)
	require.Error(t, err)
	require.Contains(t, err.Error(), "webhookDefinitions are not supported")
}

func Test_RegistryV1HelmChartProvider_Get_WebhooksWithCertProvider(t *testing.T) {
	provider := applier.RegistryV1HelmChartProvider{
		CertificateProvider:     FakeCertProvider{},
		IsWebhookSupportEnabled: true,
	}

	b := source.FromBundle(
		bundle.RegistryV1{
			CSV: clusterserviceversion.Builder().
				WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).
				WithWebhookDefinitions(v1alpha1.WebhookDescription{}).Build(),
		},
	)

	ext := &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "install-namespace",
		},
	}

	_, err := provider.Get(b, ext)
	require.NoError(t, err)
}

func Test_RegistryV1HelmChartProvider_Get_BundleRendererIntegration(t *testing.T) {
	expectedInstallNamespace := "install-namespace"
	expectedCertProvider := FakeCertProvider{}
	watchNamespace := "some-namespace"

	ext := &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "install-namespace",
			Config: &ocv1.ClusterExtensionConfig{
				ConfigType: ocv1.ClusterExtensionConfigTypeInline,
				Inline: &apiextensionsv1.JSON{
					Raw: []byte(`{"watchNamespace": "` + watchNamespace + `"}`),
				},
			},
		},
	}

	b := source.FromBundle(
		bundle.RegistryV1{
			CSV: clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces, v1alpha1.InstallModeTypeSingleNamespace).Build(),
		},
	)

	t.Run("SingleOwnNamespace install mode support off", func(t *testing.T) {
		provider := applier.RegistryV1HelmChartProvider{
			BundleRenderer: render.BundleRenderer{
				ResourceGenerators: []render.ResourceGenerator{
					func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
						// ensure correct options are being passed down to the bundle renderer
						require.Equal(t, expectedInstallNamespace, opts.InstallNamespace)
						require.Equal(t, expectedCertProvider, opts.CertificateProvider)

						// target namespaces should not set to {""} (AllNamespaces) if the SingleOwnNamespace feature flag is off
						t.Log("check that targetNamespaces option is set to AllNamespaces")
						require.Equal(t, []string{""}, opts.TargetNamespaces)
						return nil, nil
					},
				},
			},
			CertificateProvider: expectedCertProvider,
		}

		_, err := provider.Get(b, ext)
		require.NoError(t, err)
	})

	t.Run("feature on", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.SingleOwnNamespaceInstallSupport, true)

		provider := applier.RegistryV1HelmChartProvider{
			BundleRenderer: render.BundleRenderer{
				ResourceGenerators: []render.ResourceGenerator{
					func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
						// ensure correct options are being passed down to the bundle renderer
						require.Equal(t, expectedInstallNamespace, opts.InstallNamespace)
						require.Equal(t, expectedCertProvider, opts.CertificateProvider)

						// targetNamespace must be set if the feature flag is on
						t.Log("check that targetNamespaces option is set")
						require.Equal(t, []string{watchNamespace}, opts.TargetNamespaces)
						return nil, nil
					},
				},
			},
			CertificateProvider: expectedCertProvider,
		}

		_, err := provider.Get(b, ext)
		require.NoError(t, err)
	})
}

func Test_RegistryV1HelmChartProvider_Get_Success(t *testing.T) {
	provider := applier.RegistryV1HelmChartProvider{
		BundleRenderer: render.BundleRenderer{
			ResourceGenerators: []render.ResourceGenerator{
				func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
					out := make([]client.Object, 0, len(rv1.Others))
					for i := range rv1.Others {
						out = append(out, &rv1.Others[i])
					}
					return out, nil
				},
			},
		},
	}

	b := source.FromBundle(
		bundle.RegistryV1{
			CSV: clusterserviceversion.Builder().
				WithAnnotations(map[string]string{"foo": "bar"}).
				WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).Build(),
			Others: []unstructured.Unstructured{
				*ToUnstructuredT(t, &corev1.Service{
					TypeMeta: metav1.TypeMeta{
						APIVersion: corev1.SchemeGroupVersion.String(),
						Kind:       "Service",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "testService",
					},
				}),
			},
		},
	)

	ext := &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "install-namespace",
		},
	}

	chart, err := provider.Get(b, ext)
	require.NoError(t, err)
	require.NotNil(t, chart)
	require.NotNil(t, chart.Metadata)

	t.Log("Check Chart metadata contains CSV annotations")
	require.Equal(t, map[string]string{"foo": "bar"}, chart.Metadata.Annotations)

	t.Log("Check Chart templates have the same number of resources generated by the renderer")
	require.Len(t, chart.Templates, 1)
}
