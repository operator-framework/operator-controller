package applier_test

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/bundlefs"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing/clusterserviceversion"
)

func Test_RegistryV1ManifestProvider_Integration(t *testing.T) {
	t.Run("surfaces bundle source errors", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{}
		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		}
		_, err := provider.Get(fstest.MapFS{}, ext)
		require.Error(t, err)
		require.Contains(t, err.Error(), "metadata/annotations.yaml: file does not exist")
	})

	t.Run("surfaces bundle renderer errors", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			BundleRenderer: render.BundleRenderer{
				ResourceGenerators: []render.ResourceGenerator{
					func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
						return nil, errors.New("some error")
					},
				},
			},
		}

		// The contents of the bundle are not important for this tesy, only that it be a valid bundle
		// to avoid errors in the deserialization process
		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).Build()).Build()

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		}

		_, err := provider.Get(bundleFS, ext)
		require.Error(t, err)
		require.Contains(t, err.Error(), "some error")
	})

	t.Run("surfaces bundle config unmarshall errors", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			BundleRenderer: render.BundleRenderer{
				ResourceGenerators: []render.ResourceGenerator{
					func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
						return nil, nil
					},
				},
			},
			// must be true for now as we only unmarshal configuration when this feature is on
			// once we go GA and remove IsSingleOwnNamespaceEnabled it's ok to just delete this
			IsSingleOwnNamespaceEnabled: true,
		}

		// The contents of the bundle are not important for this tesy, only that it be a valid bundle
		// to avoid errors in the deserialization process
		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeSingleNamespace).Build()).Build()

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
				Config: &ocv1.ClusterExtensionConfig{
					ConfigType: ocv1.ClusterExtensionConfigTypeInline,
					Inline: &apiextensionsv1.JSON{
						Raw: []byte(`{"watchNamespace": "install-namespace"}`),
					},
				},
			},
		}

		_, err := provider.Get(bundleFS, ext)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid ClusterExtension configuration")
	})

	t.Run("returns rendered manifests", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			BundleRenderer: registryv1.Renderer,
		}
		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).Build()).
			WithBundleResource("service.yaml", &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "Service",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-service",
				},
			}).Build()
		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		}
		objs, err := provider.Get(bundleFS, ext)
		require.NoError(t, err)

		exp := ToUnstructuredT(t, &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: corev1.SchemeGroupVersion.String(),
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-service",
				Namespace: "install-namespace",
			},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{},
			},
		})

		require.Equal(t, []client.Object{exp}, objs)
	})
}

func Test_RegistryV1ManifestProvider_APIServiceSupport(t *testing.T) {
	t.Run("rejects registry+v1 bundles with API service definitions", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithOwnedAPIServiceDescriptions(v1alpha1.APIServiceDescription{Name: "test-apiservice"}).Build()).Build()

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		}

		_, err := provider.Get(bundleFS, ext)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported bundle: apiServiceDefintions are not supported")
	})
}

func Test_RegistryV1ManifestProvider_WebhookSupport(t *testing.T) {
	t.Run("rejects bundles with webhook definitions if support is disabled", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			IsWebhookSupportEnabled: false,
		}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithWebhookDefinitions(v1alpha1.WebhookDescription{}).Build()).Build()

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		}

		_, err := provider.Get(bundleFS, ext)
		require.Error(t, err)
		require.Contains(t, err.Error(), "webhookDefinitions are not supported")
	})

	t.Run("fails if bundle contains webhook definitions, webhook support is enabled, but the certificate provider is undefined", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			IsWebhookSupportEnabled: true,
			CertificateProvider:     nil,
		}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithWebhookDefinitions(v1alpha1.WebhookDescription{}).Build()).Build()

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		}

		_, err := provider.Get(bundleFS, ext)
		require.Error(t, err)
		require.Contains(t, err.Error(), "webhookDefinitions are not supported")
	})

	t.Run("accepts bundles with webhook definitions if support is enabled and a certificate provider is defined", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			CertificateProvider:     FakeCertProvider{},
			IsWebhookSupportEnabled: true,
		}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(
				clusterserviceversion.Builder().
					WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).
					WithWebhookDefinitions(v1alpha1.WebhookDescription{}).Build()).
			Build()

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		}

		_, err := provider.Get(bundleFS, ext)
		require.NoError(t, err)
	})
}

func Test_RegistryV1ManifestProvider_SingleOwnNamespaceSupport(t *testing.T) {
	t.Run("rejects bundles without AllNamespaces install mode when Single/OwnNamespace install mode support is disabled", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			IsSingleOwnNamespaceEnabled: false,
		}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeSingleNamespace).Build()).Build()

		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		})
		require.Equal(t, "unsupported bundle: bundle does not support AllNamespaces install mode", err.Error())
	})

	t.Run("rejects bundles without AllNamespaces install mode and with SingleNamespace support when Single/OwnNamespace install mode support is enabled", func(t *testing.T) {
		expectedWatchNamespace := "some-namespace"
		provider := applier.RegistryV1ManifestProvider{
			IsSingleOwnNamespaceEnabled: false,
		}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeSingleNamespace).Build()).Build()

		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
				Config: &ocv1.ClusterExtensionConfig{
					ConfigType: ocv1.ClusterExtensionConfigTypeInline,
					Inline: &apiextensionsv1.JSON{
						Raw: []byte(`{"watchNamespace": "` + expectedWatchNamespace + `"}`),
					},
				},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported bundle")
	})

	t.Run("rejects bundles without AllNamespaces install mode and with OwnNamespace support when Single/OwnNamespace install mode support is disabled", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			IsSingleOwnNamespaceEnabled: false,
		}
		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeOwnNamespace).Build()).Build()
		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported bundle")
	})

	t.Run("accepts bundles with install modes {SingleNamespace} when the appropriate configuration is given", func(t *testing.T) {
		expectedWatchNamespace := "some-namespace"
		provider := applier.RegistryV1ManifestProvider{
			BundleRenderer: render.BundleRenderer{
				ResourceGenerators: []render.ResourceGenerator{
					func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
						t.Log("ensure watch namespace is appropriately configured")
						require.Equal(t, []string{expectedWatchNamespace}, opts.TargetNamespaces)
						return nil, nil
					},
				},
			},
			IsSingleOwnNamespaceEnabled: true,
		}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeSingleNamespace).Build()).Build()

		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
				Config: &ocv1.ClusterExtensionConfig{
					ConfigType: ocv1.ClusterExtensionConfigTypeInline,
					Inline: &apiextensionsv1.JSON{
						Raw: []byte(`{"watchNamespace": "` + expectedWatchNamespace + `"}`),
					},
				},
			},
		})
		require.NoError(t, err)
	})

	t.Run("rejects bundles with {SingleNamespace} install modes when no configuration is given", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			IsSingleOwnNamespaceEnabled: true,
		}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeSingleNamespace).Build()).Build()

		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), `required field "watchNamespace" is missing`)
	})

	t.Run("accepts bundles with {OwnNamespace} install modes when the appropriate configuration is given", func(t *testing.T) {
		installNamespace := "some-namespace"
		provider := applier.RegistryV1ManifestProvider{
			BundleRenderer: render.BundleRenderer{
				ResourceGenerators: []render.ResourceGenerator{
					func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
						t.Log("ensure watch namespace is appropriately configured")
						require.Equal(t, []string{installNamespace}, opts.TargetNamespaces)
						return nil, nil
					},
				},
			},
			IsSingleOwnNamespaceEnabled: true,
		}
		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeOwnNamespace).Build()).Build()
		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: installNamespace,
				Config: &ocv1.ClusterExtensionConfig{
					ConfigType: ocv1.ClusterExtensionConfigTypeInline,
					Inline: &apiextensionsv1.JSON{
						Raw: []byte(`{"watchNamespace": "` + installNamespace + `"}`),
					},
				},
			},
		})
		require.NoError(t, err)
	})

	t.Run("rejects bundles with {OwnNamespace} install modes when no configuration is given", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			IsSingleOwnNamespaceEnabled: true,
		}
		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeOwnNamespace).Build()).Build()
		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), `required field "watchNamespace" is missing`)
	})

	t.Run("rejects bundles with {OwnNamespace} install modes when watchNamespace is not install namespace", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			IsSingleOwnNamespaceEnabled: true,
		}
		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeOwnNamespace).Build()).Build()
		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
				Config: &ocv1.ClusterExtensionConfig{
					ConfigType: ocv1.ClusterExtensionConfigTypeInline,
					Inline: &apiextensionsv1.JSON{
						Raw: []byte(`{"watchNamespace": "not-install-namespace"}`),
					},
				},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid ClusterExtension configuration:")
		require.Contains(t, err.Error(), "watchNamespace must be")
		require.Contains(t, err.Error(), "install-namespace")
	})

	t.Run("rejects bundles without AllNamespaces, SingleNamespace, or OwnNamespace install mode support when Single/OwnNamespace install mode support is enabled", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			IsSingleOwnNamespaceEnabled: true,
		}
		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeMultiNamespace).Build()).Build()
		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		})
		require.Equal(t, "unsupported bundle: bundle must support at least one of [AllNamespaces SingleNamespace OwnNamespace] install modes", err.Error())
	})
}

func Test_RegistryV1HelmChartProvider_Integration(t *testing.T) {
	t.Run("surfaces bundle source errors", func(t *testing.T) {
		provider := applier.RegistryV1HelmChartProvider{
			ManifestProvider: DummyManifestProvider,
		}
		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		}
		_, err := provider.Get(fstest.MapFS{}, ext)
		require.Error(t, err)
		require.Contains(t, err.Error(), "metadata/annotations.yaml: file does not exist")
	})

	t.Run("surfaces manifest provider failures", func(t *testing.T) {
		provider := applier.RegistryV1HelmChartProvider{
			ManifestProvider: &FakeManifestProvider{
				GetFn: func(bundle fs.FS, ext *ocv1.ClusterExtension) ([]client.Object, error) {
					return nil, errors.New("some error")
				},
			},
		}

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		}
		_, err := provider.Get(fstest.MapFS{}, ext)
		require.Error(t, err)
		require.Contains(t, err.Error(), "some error")
	})
}

func Test_RegistryV1HelmChartProvider_Chart(t *testing.T) {
	provider := applier.RegistryV1HelmChartProvider{
		ManifestProvider: &applier.RegistryV1ManifestProvider{
			BundleRenderer: registryv1.Renderer,
		},
	}

	bundleFS := bundlefs.Builder().WithPackageName("test").
		WithCSV(
			clusterserviceversion.Builder().
				WithAnnotations(map[string]string{"foo": "bar"}).
				WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).
				Build()).
		WithBundleResource("service.yaml", &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: corev1.SchemeGroupVersion.String(),
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "testService",
			},
		}).Build()

	ext := &ocv1.ClusterExtension{
		Spec: ocv1.ClusterExtensionSpec{
			Namespace: "install-namespace",
		},
	}

	chart, err := provider.Get(bundleFS, ext)
	require.NoError(t, err)
	require.NotNil(t, chart)
	require.NotNil(t, chart.Metadata)

	t.Log("Check Chart metadata contains CSV annotations")
	require.Equal(t, map[string]string{"foo": "bar"}, chart.Metadata.Annotations)

	t.Log("Check Chart templates have the same number of resources generated by the renderer")
	require.Len(t, chart.Templates, 1)
}

var DummyManifestProvider = &FakeManifestProvider{
	GetFn: func(bundle fs.FS, ext *ocv1.ClusterExtension) ([]client.Object, error) {
		return []client.Object{}, nil
	},
}

type FakeManifestProvider struct {
	GetFn func(bundleFS fs.FS, ext *ocv1.ClusterExtension) ([]client.Object, error)
}

func (f *FakeManifestProvider) Get(bundleFS fs.FS, ext *ocv1.ClusterExtension) ([]client.Object, error) {
	return f.GetFn(bundleFS, ext)
}
