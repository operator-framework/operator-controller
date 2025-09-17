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
		ce          *ocv1.ClusterExtension
		expectError bool
	}{
		{
			name: "no watch namespace is configured in a ClusterExtension CR",
			want: corev1.NamespaceAll,
			ce: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "extension",
					Annotations: nil,
				},
				Spec: ocv1.ClusterExtensionSpec{},
			},
			expectError: false,
		}, {
			name: "a watch namespace is configured in a ClusterExtension CR",
			want: "watch-namespace",
			ce: &ocv1.ClusterExtension{
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
			name: "a watch namespace is configured in a ClusterExtension CR but with invalid namespace",
			ce: &ocv1.ClusterExtension{
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
			name: "a watch namespace is configured in a ClusterExtension CR with an empty string as the namespace",
			ce: &ocv1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "extension",
				},
				Spec: ocv1.ClusterExtensionSpec{
					Config: &ocv1.ClusterExtensionConfig{
						ConfigType: ocv1.ClusterExtensionConfigTypeInline,
						Inline: &apiextensionsv1.JSON{
							Raw: []byte(`{"watchNamespace":""}`),
						},
					},
				},
			},
			expectError: true,
		}, {
			name: "an invalid watchNamespace value is configured in a ClusterExtension CR: multiple watch namespaces",
			want: "",
			ce: &ocv1.ClusterExtension{
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
			name: "an invalid watchNamespace value is configured in a ClusterExtension CR: invalid name",
			want: "",
			ce: &ocv1.ClusterExtension{
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
			name: "an invalid watchNamespace value is configured in a ClusterExtension CR: invalid json",
			want: "",
			ce: &ocv1.ClusterExtension{
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
			got, err := applier.GetWatchNamespace(tt.ce)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.expectError, err != nil)
		})
	}
}
