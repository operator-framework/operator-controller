package applier_test

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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

		// The contents of the bundle are not important for this test, only that it be a valid bundle
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

	t.Run("renders bundle with only SingleNamespace install mode in AllNamespaces mode", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			BundleRenderer: registryv1.Renderer,
		}

		// Bundle only declares SingleNamespace support - no AllNamespaces
		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().
				WithInstallModeSupportFor(v1alpha1.InstallModeTypeSingleNamespace).
				WithStrategyDeploymentSpecs(v1alpha1.StrategyDeploymentSpec{
					Name: "test-operator",
				}).Build()).Build()

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		}

		objs, err := provider.Get(bundleFS, ext)
		require.NoError(t, err, "bundles without AllNamespaces support should still render successfully")
		requireTargetNamespacesAnnotation(t, objs, corev1.NamespaceAll)
	})

	t.Run("renders bundle with only OwnNamespace install mode in AllNamespaces mode", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			BundleRenderer: registryv1.Renderer,
		}

		// Bundle only declares OwnNamespace support - no AllNamespaces
		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().
				WithInstallModeSupportFor(v1alpha1.InstallModeTypeOwnNamespace).
				WithStrategyDeploymentSpecs(v1alpha1.StrategyDeploymentSpec{
					Name: "test-operator",
				}).Build()).Build()

		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
			},
		}

		objs, err := provider.Get(bundleFS, ext)
		require.NoError(t, err, "bundles without AllNamespaces support should still render successfully")
		requireTargetNamespacesAnnotation(t, objs, corev1.NamespaceAll)
	})

	t.Run("returns terminal error for invalid config", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			BundleRenderer: render.BundleRenderer{
				ResourceGenerators: []render.ResourceGenerator{
					func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
						return nil, nil
					},
				},
			},
		}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).Build()).Build()

		// ClusterExtension with invalid config - unknown field
		ext := &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
				Config: &ocv1.ClusterExtensionConfig{
					ConfigType: ocv1.ClusterExtensionConfigTypeInline,
					Inline: &apiextensionsv1.JSON{
						Raw: []byte(`{"unknownField": "value"}`),
					},
				},
			},
		}

		_, err := provider.Get(bundleFS, ext)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid ClusterExtension configuration")
		// Assert that config validation errors are terminal (not retriable)
		require.ErrorIs(t, err, reconcile.TerminalError(nil), "config validation errors should be terminal")
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

func Test_RegistryV1ManifestProvider_DeploymentConfig(t *testing.T) {
	t.Run("passes deploymentConfig to renderer when provided in configuration", func(t *testing.T) {
		expectedEnvVars := []corev1.EnvVar{
			{Name: "TEST_ENV", Value: "test-value"},
		}
		provider := applier.RegistryV1ManifestProvider{
			BundleRenderer: render.BundleRenderer{
				ResourceGenerators: []render.ResourceGenerator{
					func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
						t.Log("ensure deploymentConfig is passed to renderer")
						require.NotNil(t, opts.DeploymentConfig)
						require.Equal(t, expectedEnvVars, opts.DeploymentConfig.Env)
						return nil, nil
					},
				},
			},
			IsDeploymentConfigEnabled: true,
		}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).Build()).Build()

		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
				Config: &ocv1.ClusterExtensionConfig{
					ConfigType: ocv1.ClusterExtensionConfigTypeInline,
					Inline: &apiextensionsv1.JSON{
						Raw: []byte(`{"deploymentConfig": {"env": [{"name": "TEST_ENV", "value": "test-value"}]}}`),
					},
				},
			},
		})
		require.NoError(t, err)
	})

	t.Run("does not pass deploymentConfig to renderer when not provided in configuration", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			BundleRenderer: render.BundleRenderer{
				ResourceGenerators: []render.ResourceGenerator{
					func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
						t.Log("ensure deploymentConfig is nil when not provided")
						require.Nil(t, opts.DeploymentConfig)
						return nil, nil
					},
				},
			},
			IsDeploymentConfigEnabled: true,
		}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).Build()).Build()

		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
				// No config provided
			},
		})
		require.NoError(t, err)
	})

	t.Run("passes deploymentConfig with multiple fields to renderer", func(t *testing.T) {
		expectedNodeSelector := map[string]string{"kubernetes.io/os": "linux"}
		expectedTolerations := []corev1.Toleration{
			{Key: "key1", Operator: "Equal", Value: "value1", Effect: "NoSchedule"},
		}
		provider := applier.RegistryV1ManifestProvider{
			BundleRenderer: render.BundleRenderer{
				ResourceGenerators: []render.ResourceGenerator{
					func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
						t.Log("ensure all deploymentConfig fields are passed to renderer")
						require.NotNil(t, opts.DeploymentConfig)
						require.Equal(t, expectedNodeSelector, opts.DeploymentConfig.NodeSelector)
						require.Equal(t, expectedTolerations, opts.DeploymentConfig.Tolerations)
						return nil, nil
					},
				},
			},
			IsDeploymentConfigEnabled: true,
		}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).Build()).Build()

		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
				Config: &ocv1.ClusterExtensionConfig{
					ConfigType: ocv1.ClusterExtensionConfigTypeInline,
					Inline: &apiextensionsv1.JSON{
						Raw: []byte(`{
							"deploymentConfig": {
								"nodeSelector": {"kubernetes.io/os": "linux"},
								"tolerations": [{"key": "key1", "operator": "Equal", "value": "value1", "effect": "NoSchedule"}]
							}
						}`),
					},
				},
			},
		})
		require.NoError(t, err)
	})

	t.Run("handles empty deploymentConfig gracefully", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			BundleRenderer: render.BundleRenderer{
				ResourceGenerators: []render.ResourceGenerator{
					func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
						t.Log("ensure deploymentConfig is nil for empty config object")
						require.Nil(t, opts.DeploymentConfig)
						return nil, nil
					},
				},
			},
			IsDeploymentConfigEnabled: true,
		}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).Build()).Build()

		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
				Config: &ocv1.ClusterExtensionConfig{
					ConfigType: ocv1.ClusterExtensionConfigTypeInline,
					Inline: &apiextensionsv1.JSON{
						Raw: []byte(`{"deploymentConfig": {}}`),
					},
				},
			},
		})
		require.NoError(t, err)
	})

	t.Run("returns terminal error when deploymentConfig has invalid structure", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			BundleRenderer: render.BundleRenderer{
				ResourceGenerators: []render.ResourceGenerator{
					func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
						return nil, nil
					},
				},
			},
			IsDeploymentConfigEnabled: true,
		}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).Build()).Build()

		// Provide deploymentConfig with invalid structure - env should be array, not string
		// Schema validation catches this before conversion
		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
				Config: &ocv1.ClusterExtensionConfig{
					ConfigType: ocv1.ClusterExtensionConfigTypeInline,
					Inline: &apiextensionsv1.JSON{
						Raw: []byte(`{"deploymentConfig": {"env": "not-an-array"}}`),
					},
				},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid ClusterExtension configuration")
		require.Contains(t, err.Error(), "deploymentConfig.env")
		require.ErrorIs(t, err, reconcile.TerminalError(nil), "config validation errors should be terminal")
	})

	t.Run("returns terminal error when deploymentConfig is used but feature gate is disabled", func(t *testing.T) {
		provider := applier.RegistryV1ManifestProvider{
			BundleRenderer: render.BundleRenderer{
				ResourceGenerators: []render.ResourceGenerator{
					func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
						return nil, nil
					},
				},
			},
			IsDeploymentConfigEnabled: false,
		}

		bundleFS := bundlefs.Builder().WithPackageName("test").
			WithCSV(clusterserviceversion.Builder().WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces).Build()).Build()

		_, err := provider.Get(bundleFS, &ocv1.ClusterExtension{
			Spec: ocv1.ClusterExtensionSpec{
				Namespace: "install-namespace",
				Config: &ocv1.ClusterExtensionConfig{
					ConfigType: ocv1.ClusterExtensionConfigTypeInline,
					Inline: &apiextensionsv1.JSON{
						Raw: []byte(`{"deploymentConfig": {"env": [{"name": "TEST_ENV", "value": "test-value"}]}}`),
					},
				},
			},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown field \"deploymentConfig\"")
		require.ErrorIs(t, err, reconcile.TerminalError(nil), "feature gate disabled error should be terminal")
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

func requireTargetNamespacesAnnotation(t *testing.T, objs []client.Object, expected string) {
	t.Helper()
	var found int
	for _, obj := range objs {
		dep, ok := obj.(*appsv1.Deployment)
		if !ok {
			continue
		}
		found++
		annotations := dep.Spec.Template.Annotations
		v, exists := annotations["olm.targetNamespaces"]
		require.True(t, exists, "deployment %q missing olm.targetNamespaces annotation on pod template", dep.Name)
		require.Equal(t, expected, v, "olm.targetNamespaces annotation on deployment %q", dep.Name)
	}
	require.Positive(t, found, "expected at least one Deployment in rendered objects")
}
