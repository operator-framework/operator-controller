package convert_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/bundle/source"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	. "github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util/testing"
)

func Test_BundleToHelmChartConverter_ToHelmChart_ReturnsBundleSourceFailures(t *testing.T) {
	converter := convert.BundleToHelmChartConverter{}
	var failingBundleSource FakeBundleSource = func() (bundle.RegistryV1, error) {
		return bundle.RegistryV1{}, errors.New("some error")
	}
	_, err := converter.ToHelmChart(failingBundleSource, "install-namespace", "watch-namespace")
	require.Error(t, err)
	require.Contains(t, err.Error(), "some error")
}

func Test_BundleToHelmChartConverter_ToHelmChart_ReturnsBundleRendererFailures(t *testing.T) {
	converter := convert.BundleToHelmChartConverter{
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
			CSV: MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
		},
	)

	_, err := converter.ToHelmChart(b, "install-namespace", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "some error")
}

func Test_BundleToHelmChartConverter_ToHelmChart_NoAPIServiceDefinitions(t *testing.T) {
	converter := convert.BundleToHelmChartConverter{}

	b := source.FromBundle(
		bundle.RegistryV1{
			CSV: MakeCSV(WithOwnedAPIServiceDescriptions(v1alpha1.APIServiceDescription{})),
		},
	)

	_, err := converter.ToHelmChart(b, "install-namespace", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported bundle: apiServiceDefintions are not supported")
}

func Test_BundleToHelmChartConverter_ToHelmChart_NoWebhooksWithoutCertProvider(t *testing.T) {
	converter := convert.BundleToHelmChartConverter{
		IsWebhookSupportEnabled: true,
	}

	b := source.FromBundle(
		bundle.RegistryV1{
			CSV: MakeCSV(WithWebhookDefinitions(v1alpha1.WebhookDescription{})),
		},
	)

	_, err := converter.ToHelmChart(b, "install-namespace", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "webhookDefinitions are not supported")
}

func Test_BundleToHelmChartConverter_ToHelmChart_WebhooksSupportDisabled(t *testing.T) {
	converter := convert.BundleToHelmChartConverter{
		IsWebhookSupportEnabled: false,
	}

	b := source.FromBundle(
		bundle.RegistryV1{
			CSV: MakeCSV(WithWebhookDefinitions(v1alpha1.WebhookDescription{})),
		},
	)

	_, err := converter.ToHelmChart(b, "install-namespace", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "webhookDefinitions are not supported")
}

func Test_BundleToHelmChartConverter_ToHelmChart_WebhooksWithCertProvider(t *testing.T) {
	converter := convert.BundleToHelmChartConverter{
		CertificateProvider:     FakeCertProvider{},
		IsWebhookSupportEnabled: true,
	}

	b := source.FromBundle(
		bundle.RegistryV1{
			CSV: MakeCSV(
				WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces),
				WithWebhookDefinitions(v1alpha1.WebhookDescription{}),
			),
		},
	)

	_, err := converter.ToHelmChart(b, "install-namespace", "")
	require.NoError(t, err)
}

func Test_BundleToHelmChartConverter_ToHelmChart_BundleRendererIntegration(t *testing.T) {
	expectedInstallNamespace := "install-namespace"
	expectedWatchNamespace := ""
	expectedCertProvider := FakeCertProvider{}

	converter := convert.BundleToHelmChartConverter{
		BundleRenderer: render.BundleRenderer{
			ResourceGenerators: []render.ResourceGenerator{
				func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
					// ensure correct options are being passed down to the bundle renderer
					require.Equal(t, expectedInstallNamespace, opts.InstallNamespace)
					require.Equal(t, []string{expectedWatchNamespace}, opts.TargetNamespaces)
					require.Equal(t, expectedCertProvider, opts.CertificateProvider)
					return nil, nil
				},
			},
		},
		CertificateProvider: expectedCertProvider,
	}

	b := source.FromBundle(
		bundle.RegistryV1{
			CSV: MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
		},
	)

	_, err := converter.ToHelmChart(b, expectedInstallNamespace, expectedWatchNamespace)
	require.NoError(t, err)
}

func Test_BundleToHelmChartConverter_ToHelmChart_Success(t *testing.T) {
	converter := convert.BundleToHelmChartConverter{
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
			CSV: MakeCSV(
				WithAnnotations(map[string]string{"foo": "bar"}),
				WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces),
			),
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

	chart, err := converter.ToHelmChart(b, "install-namespace", "")
	require.NoError(t, err)
	require.NotNil(t, chart)
	require.NotNil(t, chart.Metadata)

	t.Log("Check Chart metadata contains CSV annotations")
	require.Equal(t, map[string]string{"foo": "bar"}, chart.Metadata.Annotations)

	t.Log("Check Chart templates have the same number of resources generated by the renderer")
	require.Len(t, chart.Templates, 1)
}

func Test_BundleToHelmChartConverter_ForwardsConfigToRenderer(t *testing.T) {
	expectedCfg := map[string]interface{}{
		"version": "v2.0.0-demo",
		"name":    "demo-configmap",
	}

	converter := convert.BundleToHelmChartConverter{
		BundleRenderer: render.BundleRenderer{
			ResourceGenerators: []render.ResourceGenerator{
				func(rv1 *bundle.RegistryV1, opts render.Options) ([]client.Object, error) {
					// generator emulates BundleAdditionalResourcesGenerator behavior
					cm := &corev1.ConfigMap{
						TypeMeta: metav1.TypeMeta{
							APIVersion: corev1.SchemeGroupVersion.String(),
							Kind:       "ConfigMap",
						},
						ObjectMeta: metav1.ObjectMeta{Name: "test-configmap"},
						Data:       map[string]string{},
					}
					if opts.Config != nil {
						for k, v := range opts.Config {
							cm.Data[k] = fmt.Sprintf("%v", v)
						}
					}
					return []client.Object{cm}, nil
				},
			},
		},
	}

	b := source.FromBundle(
		bundle.RegistryV1{
			CSV: MakeCSV(WithInstallModeSupportFor(v1alpha1.InstallModeTypeAllNamespaces)),
		},
	)

	chart, err := converter.ToHelmChartWithConfig(b, "install-namespace", "", expectedCfg)
	require.NoError(t, err)
	require.NotNil(t, chart)

	found := false
	for _, f := range chart.Files {
		var obj map[string]interface{}
		require.NoError(t, json.Unmarshal(f.Data, &obj))
		if kind, ok := obj["kind"].(string); ok && kind == "ConfigMap" {
			data, ok := obj["data"].(map[string]interface{})
			require.True(t, ok)
			require.Equal(t, expectedCfg["version"], data["version"])
			require.Equal(t, expectedCfg["name"], data["name"])
			found = true
			break
		}
	}
	require.True(t, found, "expected ConfigMap not found in chart files")
}
