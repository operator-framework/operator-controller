package applier_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

func TestGetWatchNamespacesWhenFeatureGateIsDisabled(t *testing.T) {
	watchNamespace, err := applier.GetWatchNamespace(&ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "extension",
		},
		Spec: ocv1.ClusterExtensionSpec{
			Config: &ocv1.ClusterExtensionConfig{
				ConfigType: ocv1.ClusterExtensionConfigTypeInline,
				Inline: &apiextensionsv1.JSON{
					Raw: []byte(`{"watchNamespace":"watch-namespace"}`),
				},
			},
		},
	})
	require.NoError(t, err)
	t.Log("Check watchNamespace is '' even if the configuration is set")
	require.Equal(t, corev1.NamespaceAll, watchNamespace)
}

func TestGetWatchNamespace(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.SingleOwnNamespaceInstallSupport, true)

	for _, tt := range []struct {
		name        string
		want        string
		csv         *ocv1.ClusterExtension
		expectError bool
	}{
		{
			name: "cluster extension does not configure a watch namespace",
			want: corev1.NamespaceAll,
			csv: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "extension",
					Annotations: nil,
				},
				Spec: ocv1.ClusterExtensionSpec{},
			},
			expectError: false,
		}, {
			name: "cluster extension configures a watch namespace",
			want: "watch-namespace",
			csv: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "extension",
				},
				Spec: ocv1.ClusterExtensionSpec{
					Config: &ocv1.ClusterExtensionConfig{
						ConfigType: ocv1.ClusterExtensionConfigTypeInline,
						Inline: &apiextensionsv1.JSON{
							Raw: []byte(`{"watchNamespace":"watch-namespace"}`),
						},
					},
				},
			},
			expectError: false,
		}, {
			name: "cluster extension configures a watch namespace through annotation",
			want: "watch-namespace",
			csv: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "extension",
					Annotations: map[string]string{
						"olm.operatorframework.io/watch-namespace": "watch-namespace",
					},
				},
			},
			expectError: false,
		}, {
			name: "cluster extension configures a watch namespace through annotation with invalid ns",
			csv: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "extension",
					Annotations: map[string]string{
						"olm.operatorframework.io/watch-namespace": "watch-namespace-",
					},
				},
			},
			expectError: true,
		}, {
			name: "cluster extension configures a watch namespace through annotation with empty ns",
			csv: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "extension",
					Annotations: map[string]string{
						"olm.operatorframework.io/watch-namespace": "",
					},
				},
			},
			expectError: true,
		}, {
			name: "cluster extension configures a watch namespace through annotation and config (take config)",
			want: "watch-namespace",
			csv: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "extension",
					Annotations: map[string]string{
						"olm.operatorframework.io/watch-namespace": "dont-use-this-watch-namespace",
					},
				},
				Spec: ocv1.ClusterExtensionSpec{
					Config: &ocv1.ClusterExtensionConfig{
						ConfigType: ocv1.ClusterExtensionConfigTypeInline,
						Inline: &apiextensionsv1.JSON{
							Raw: []byte(`{"watchNamespace":"watch-namespace"}`),
						},
					},
				},
			},
			expectError: false,
		}, {
			name: "cluster extension configures an invalid watchNamespace: multiple watch namespaces",
			want: "",
			csv: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "extension",
				},
				Spec: ocv1.ClusterExtensionSpec{
					Config: &ocv1.ClusterExtensionConfig{
						ConfigType: ocv1.ClusterExtensionConfigTypeInline,
						Inline: &apiextensionsv1.JSON{
							Raw: []byte(`{"watchNamespace":"watch-namespace,watch-namespace2,watch-namespace3"}`),
						},
					},
				},
			},
			expectError: true,
		}, {
			name: "cluster extension configures an invalid watchNamespace: invalid name",
			want: "",
			csv: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "extension",
				},
				Spec: ocv1.ClusterExtensionSpec{
					Config: &ocv1.ClusterExtensionConfig{
						ConfigType: ocv1.ClusterExtensionConfigTypeInline,
						Inline: &apiextensionsv1.JSON{
							Raw: []byte(`{"watchNamespace":"watch-namespace-"}`),
						},
					},
				},
			},
			expectError: true,
		}, {
			name: "cluster extension configures an invalid watchNamespace: invalid json",
			want: "",
			csv: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "extension",
				},
				Spec: ocv1.ClusterExtensionSpec{
					Config: &ocv1.ClusterExtensionConfig{
						ConfigType: ocv1.ClusterExtensionConfigTypeInline,
						Inline: &apiextensionsv1.JSON{
							Raw: []byte(`invalid json`),
						},
					},
				},
			},
			expectError: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applier.GetWatchNamespace(tt.csv)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.expectError, err != nil)
		})
	}
}
