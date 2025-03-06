package applier_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	v1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

func TestGetWatchNamespacesWhenFeatureGateIsDisabled(t *testing.T) {
	watchNamespace, err := applier.GetWatchNamespace(&v1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name: "extension",
			// Annotations: map[string]string{
			// 	"olm.operatorframework.io/watch-namespace": "watch-namespace",
			// },
		},
		Spec: v1.ClusterExtensionSpec{
			Config: []runtime.RawExtension{
				{Raw: []byte(
					`{"apiVersion":"olm.operatorframework.io/v1","kind":"BundleConfig",` +
						`"spec":{"watchNamespace":"watch-namespace"}}`)},
			},
		},
	})
	require.NoError(t, err)
	t.Log("Check watchNamespace is '' even if the annotation is set")
	require.Equal(t, corev1.NamespaceAll, watchNamespace)
}

func TestGetWatchNamespace(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, features.OperatorControllerFeatureGate, features.SingleOwnNamespaceInstallSupport, true)

	for _, tt := range []struct {
		name        string
		want        string
		csv         *v1.ClusterExtension
		expectError bool
	}{
		{
			name: "cluster extension does not have watch namespace annotation",
			want: corev1.NamespaceAll,
			csv: &v1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "extension",
					Annotations: nil,
				},
				Spec: v1.ClusterExtensionSpec{},
			},
			expectError: false,
		}, {
			name: "cluster extension has valid namespace annotation",
			want: "watch-namespace",
			csv: &v1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "extension",
				},
				Spec: v1.ClusterExtensionSpec{
					Config: []runtime.RawExtension{
						{Raw: []byte(
							`{"apiVersion":"olm.operatorframework.io/v1","kind":"BundleConfig",` +
								`"spec":{"watchNamespace":"watch-namespace"}}`)},
					},
				},
			},
			expectError: false,
		}, {
			name: "cluster extension has invalid namespace annotation: multiple watch namespaces",
			want: "",
			csv: &v1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "extension",
				},
				Spec: v1.ClusterExtensionSpec{
					Config: []runtime.RawExtension{
						{Raw: []byte(
							`{"apiVersion":"olm.operatorframework.io/v1","kind":"BundleConfig",` +
								`"spec":{"watchNamespace":"watch-namespace,watch-namespace2,watch-namespace3"}}`)},
					},
				},
			},
			expectError: true,
		}, {
			name: "cluster extension has invalid namespace annotation: invalid name",
			want: "",
			csv: &v1.ClusterExtension{
				ObjectMeta: metav1.ObjectMeta{
					Name: "extension",
					Annotations: map[string]string{
						"olm.operatorframework.io/watch-namespace": "watch-namespace-",
					},
				},
				Spec: v1.ClusterExtensionSpec{
					Config: []runtime.RawExtension{
						{Raw: []byte(
							`{"apiVersion":"olm.operatorframework.io/v1","kind":"BundleConfig",` +
								`"spec":{"watchNamespace":"watch-namespace-"}}`)},
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
