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
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "ClusterRoleBinding",
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "RoleBinding",
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "Role",
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
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "Role",
								},
							},
						},
					},
				},
				{
					Name: string(applier.PhaseRBACBindings),
					Objects: []v1.ClusterExtensionRevisionObject{
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "ClusterRoleBinding",
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "RoleBinding",
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
									"apiVersion": "v1",
									"kind":       "ConfigMap",
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
					},
				},
			},
		},
		{
			name: "no objects",
			objs: []v1.ClusterExtensionRevisionObject{},
			want: []v1.ClusterExtensionRevisionPhase{},
		},
		{
			name: "sort by group within same phase",
			objs: []v1.ClusterExtensionRevisionObject{
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
							"metadata": map[string]interface{}{
								"name": "test",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name": "test",
							},
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
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"name": "test",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "apps/v1",
									"kind":       "Deployment",
									"metadata": map[string]interface{}{
										"name": "test",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "sort by version within same group and phase",
			objs: []v1.ClusterExtensionRevisionObject{
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "batch/v1",
							"kind":       "Job",
							"metadata": map[string]interface{}{
								"name": "test",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "batch/v1beta1",
							"kind":       "CronJob",
							"metadata": map[string]interface{}{
								"name": "test",
							},
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
									"apiVersion": "batch/v1",
									"kind":       "Job",
									"metadata": map[string]interface{}{
										"name": "test",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "batch/v1beta1",
									"kind":       "CronJob",
									"metadata": map[string]interface{}{
										"name": "test",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "sort by kind within same group, version, and phase",
			objs: []v1.ClusterExtensionRevisionObject{
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Service",
							"metadata": map[string]interface{}{
								"name": "test",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name": "test",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Secret",
							"metadata": map[string]interface{}{
								"name": "test",
							},
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
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"name": "test",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "Secret",
									"metadata": map[string]interface{}{
										"name": "test",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "Service",
									"metadata": map[string]interface{}{
										"name": "test",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "sort by namespace within same GVK and phase",
			objs: []v1.ClusterExtensionRevisionObject{
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "test",
								"namespace": "zebra",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "test",
								"namespace": "alpha",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "test",
								"namespace": "beta",
							},
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
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"name":      "test",
										"namespace": "alpha",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"name":      "test",
										"namespace": "beta",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"name":      "test",
										"namespace": "zebra",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "sort by name within same GVK, namespace, and phase",
			objs: []v1.ClusterExtensionRevisionObject{
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "zoo",
								"namespace": "default",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "apple",
								"namespace": "default",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "banana",
								"namespace": "default",
							},
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
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"name":      "apple",
										"namespace": "default",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"name":      "banana",
										"namespace": "default",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"name":      "zoo",
										"namespace": "default",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "comprehensive sorting - all dimensions",
			objs: []v1.ClusterExtensionRevisionObject{
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
							"metadata": map[string]interface{}{
								"name":      "app-z",
								"namespace": "prod",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Secret",
							"metadata": map[string]interface{}{
								"name":      "secret-b",
								"namespace": "prod",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Secret",
							"metadata": map[string]interface{}{
								"name":      "secret-a",
								"namespace": "prod",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "config",
								"namespace": "dev",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
							"metadata": map[string]interface{}{
								"name":      "app-a",
								"namespace": "prod",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "config",
								"namespace": "prod",
							},
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
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"name":      "config",
										"namespace": "dev",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"name":      "config",
										"namespace": "prod",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "Secret",
									"metadata": map[string]interface{}{
										"name":      "secret-a",
										"namespace": "prod",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "Secret",
									"metadata": map[string]interface{}{
										"name":      "secret-b",
										"namespace": "prod",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "apps/v1",
									"kind":       "Deployment",
									"metadata": map[string]interface{}{
										"name":      "app-a",
										"namespace": "prod",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "apps/v1",
									"kind":       "Deployment",
									"metadata": map[string]interface{}{
										"name":      "app-z",
										"namespace": "prod",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "cluster-scoped vs namespaced resources - empty namespace sorts first",
			objs: []v1.ClusterExtensionRevisionObject{
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "ClusterRole",
							"metadata": map[string]interface{}{
								"name": "admin",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "ClusterRole",
							"metadata": map[string]interface{}{
								"name": "viewer",
							},
						},
					},
				},
				{
					Object: unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "Role",
							"metadata": map[string]interface{}{
								"name":      "admin",
								"namespace": "default",
							},
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
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "ClusterRole",
									"metadata": map[string]interface{}{
										"name": "admin",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "ClusterRole",
									"metadata": map[string]interface{}{
										"name": "viewer",
									},
								},
							},
						},
						{
							Object: unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "Role",
									"metadata": map[string]interface{}{
										"name":      "admin",
										"namespace": "default",
									},
								},
							},
						},
					},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, applier.PhaseSort(tt.objs))
		})
	}
}
