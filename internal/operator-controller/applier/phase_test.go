package applier_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"

	ocv1ac "github.com/operator-framework/operator-controller/applyconfigurations/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/applier"
)

func Test_PhaseSort(t *testing.T) {
	for _, tt := range []struct {
		name string
		objs []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration
		want []*ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration
	}{
		{
			name: "single deploy obj",
			objs: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
						},
					},
				},
			},
			want: []*ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apiregistration.k8s.io/v1",
							"kind":       "APIService",
						},
					},
				},
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
						},
					},
				},
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Namespace",
						},
					},
				},
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "some.api/v1",
							"kind":       "SomeCustomResource",
						},
					},
				},
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "ClusterRole",
						},
					},
				},
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "ClusterRoleBinding",
						},
					},
				},
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "RoleBinding",
						},
					},
				},
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "Role",
						},
					},
				},
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "PersistentVolume",
						},
					},
				},
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "networking.k8s.io/v1",
							"kind":       "NetworkPolicy",
						},
					},
				},
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apiextensions.k8s.io/v1",
							"kind":       "CustomResourceDefinition",
						},
					},
				},
			},
			want: []*ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseNamespaces)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "Namespace",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhasePolicies)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "networking.k8s.io/v1",
									"kind":       "NetworkPolicy",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseRBAC)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "ClusterRole",
								},
							},
						},
						{
							Object: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "Role",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseRBACBindings)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "ClusterRoleBinding",
								},
							},
						},
						{
							Object: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "RoleBinding",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseCRDs)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "apiextensions.k8s.io/v1",
									"kind":       "CustomResourceDefinition",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseStorage)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "PersistentVolume",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "apps/v1",
									"kind":       "Deployment",
								},
							},
						},
						{
							Object: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "some.api/v1",
									"kind":       "SomeCustomResource",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhasePublish)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
						},
					},
				},
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
						},
					},
				},
				{
					Object: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ServiceAccount",
						},
					},
				},
			},
			want: []*ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseRBAC)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ServiceAccount",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ConfigMap",
								},
							},
						},
						{
							Object: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{},
			want: []*ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration{},
		},
		{
			name: "sort by group within same phase",
			objs: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
				{
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
			want: []*ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
				{
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
			want: []*ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
				{
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
			want: []*ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
				{
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
			want: []*ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
				{
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
			want: []*ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
				{
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
			want: []*ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
				{
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
					Object: &unstructured.Unstructured{
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
			want: []*ocv1ac.ClusterExtensionRevisionPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseRBAC)),
					Objects: []ocv1ac.ClusterExtensionRevisionObjectApplyConfiguration{
						{
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
							Object: &unstructured.Unstructured{
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
