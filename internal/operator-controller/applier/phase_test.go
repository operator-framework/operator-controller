package applier_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
)

func Test_PhaseSort(t *testing.T) {
	for _, tt := range []struct {
		name string
		objs []v1.ClusterExtensionRevisionObject
		want []v1.ClusterExtensionRevisionPhase
	}{
		{
			name: "single deploy obj",
			objs: []v1.ClusterExtensionRevisionObject{
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
						},
					},
				},
			},
			want: []v1.ClusterExtensionRevisionPhase{
				{
					Name: string(applier.PhaseDeploy),
					Objects: []v1.ClusterExtensionRevisionObject{
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "apps/v1",
									"kind":       "Deployment",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "all phases",
			objs: []v1.ClusterExtensionRevisionObject{
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apiregistration.k8s.io/v1",
							"kind":       "APIService",
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Namespace",
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "some.api/v1",
							"kind":       "SomeCustomResource",
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "ClusterRole",
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "PersistentVolume",
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "networking.k8s.io/v1",
							"kind":       "NetworkPolicy",
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apiextensions.k8s.io/v1",
							"kind":       "CustomResourceDefinition",
						},
					},
				},
			},
			want: []v1.ClusterExtensionRevisionPhase{
				{
					Name: string(applier.PhaseNamespaces),
					Objects: []v1.ClusterExtensionRevisionObject{
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "Namespace",
								},
							},
						},
					},
				},
				{
					Name: string(applier.PhasePolicies),
					Objects: []v1.ClusterExtensionRevisionObject{
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "networking.k8s.io/v1",
									"kind":       "NetworkPolicy",
								},
							},
						},
					},
				},
				{
					Name: string(applier.PhaseRBAC),
					Objects: []v1.ClusterExtensionRevisionObject{
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "ClusterRole",
								},
							},
						},
					},
				},
				{
					Name: string(applier.PhaseCRDs),
					Objects: []v1.ClusterExtensionRevisionObject{
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "apiextensions.k8s.io/v1",
									"kind":       "CustomResourceDefinition",
								},
							},
						},
					},
				},
				{
					Name: string(applier.PhaseStorage),
					Objects: []v1.ClusterExtensionRevisionObject{
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "PersistentVolume",
								},
							},
						},
					},
				},
				{
					Name: string(applier.PhaseDeploy),
					Objects: []v1.ClusterExtensionRevisionObject{
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "apps/v1",
									"kind":       "Deployment",
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "some.api/v1",
									"kind":       "SomeCustomResource",
								},
							},
						},
					},
				},
				{
					Name: string(applier.PhasePublish),
					Objects: []v1.ClusterExtensionRevisionObject{
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "apiregistration.k8s.io/v1",
									"kind":       "APIService",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "sorted and batched",
			objs: []v1.ClusterExtensionRevisionObject{
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ServiceAccount",
						},
					},
				},
			},
			want: []v1.ClusterExtensionRevisionPhase{
				{
					Name: string(applier.PhaseRBAC),
					Objects: []v1.ClusterExtensionRevisionObject{
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ServiceAccount",
								},
							},
						},
					},
				},
				{
					Name: string(applier.PhaseDeploy),
					Objects: []v1.ClusterExtensionRevisionObject{
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "apps/v1",
									"kind":       "Deployment",
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ConfigMap",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "no objects",
			objs: []v1.ClusterExtensionRevisionObject{},
			want: []v1.ClusterExtensionRevisionPhase{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, applier.PhaseSort(tt.objs))
		})
	}
}
