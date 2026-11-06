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
		objs []ocv1ac.ClusterObjectSetObjectApplyConfiguration
		want []*ocv1ac.ClusterObjectSetPhaseApplyConfiguration
	}{
		{
			name: "single deploy obj",
			objs: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
						},
					},
				},
			},
			want: []*ocv1ac.ClusterObjectSetPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "admissionregistration.k8s.io/v1",
							"kind":       "ValidatingWebhookConfiguration",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Namespace",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "some.api/v1",
							"kind":       "SomeCustomResource",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "ClusterRole",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "ClusterRoleBinding",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "RoleBinding",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "Role",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "PersistentVolume",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "networking.k8s.io/v1",
							"kind":       "NetworkPolicy",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apiextensions.k8s.io/v1",
							"kind":       "CustomResourceDefinition",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ServiceAccount",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Secret",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Service",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "cert-manager.io/v1",
							"kind":       "Certificate",
							"metadata": map[string]interface{}{
								"name": "test",
							},
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "autoscaling.k8s.io/v1",
							"kind":       "VerticalPodAutoscaler",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "monitoring.coreos.com/v1",
							"kind":       "PrometheusRule",
						},
					},
				},
			},
			want: []*ocv1ac.ClusterObjectSetPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseNamespaces)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
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
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "networking.k8s.io/v1",
									"kind":       "NetworkPolicy",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseIdentity)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ServiceAccount",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseConfiguration)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "Secret",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseStorage)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "PersistentVolume",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseCRDs)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "apiextensions.k8s.io/v1",
									"kind":       "CustomResourceDefinition",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseRoles)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "ClusterRole",
								},
							},
						},
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "Role",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseBindings)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "ClusterRoleBinding",
								},
							},
						},
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "rbac.authorization.k8s.io/v1",
									"kind":       "RoleBinding",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseInfrastructure)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "Service",
								},
							},
						},
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "cert-manager.io/v1",
									"kind":       "Certificate",
									"metadata": map[string]interface{}{
										"name": "test",
									},
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "apps/v1",
									"kind":       "Deployment",
								},
							},
						},
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "some.api/v1",
									"kind":       "SomeCustomResource",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseScaling)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "autoscaling.k8s.io/v1",
									"kind":       "VerticalPodAutoscaler",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhasePublish)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "monitoring.coreos.com/v1",
									"kind":       "PrometheusRule",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseAdmission)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "admissionregistration.k8s.io/v1",
									"kind":       "ValidatingWebhookConfiguration",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "sorted and batched",
			objs: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "apps/v1",
							"kind":       "Deployment",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
						},
					},
				},
				{
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ServiceAccount",
						},
					},
				},
			},
			want: []*ocv1ac.ClusterObjectSetPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseIdentity)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ServiceAccount",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseConfiguration)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ConfigMap",
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{},
			want: []*ocv1ac.ClusterObjectSetPhaseApplyConfiguration{},
		},
		{
			name: "sort by group across infrastructure and deploy phases",
			objs: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
				{
					InlineManifest: &unstructured.Unstructured{
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
					InlineManifest: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "cert-manager.io/v1",
							"kind":       "Certificate",
							"metadata": map[string]interface{}{
								"name": "test",
							},
						},
					},
				},
			},
			want: []*ocv1ac.ClusterObjectSetPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseInfrastructure)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"apiVersion": "cert-manager.io/v1",
									"kind":       "Certificate",
									"metadata": map[string]interface{}{
										"name": "test",
									},
								},
							},
						},
					},
				},
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
				{
					InlineManifest: &unstructured.Unstructured{
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
					InlineManifest: &unstructured.Unstructured{
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
			want: []*ocv1ac.ClusterObjectSetPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
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
							InlineManifest: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
				{
					InlineManifest: &unstructured.Unstructured{
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
					InlineManifest: &unstructured.Unstructured{
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
			want: []*ocv1ac.ClusterObjectSetPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseConfiguration)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
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
							InlineManifest: &unstructured.Unstructured{
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
				},
			},
		},
		{
			name: "sort by namespace within same GVK and phase",
			objs: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
				{
					InlineManifest: &unstructured.Unstructured{
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
					InlineManifest: &unstructured.Unstructured{
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
					InlineManifest: &unstructured.Unstructured{
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
			want: []*ocv1ac.ClusterObjectSetPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseConfiguration)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
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
							InlineManifest: &unstructured.Unstructured{
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
							InlineManifest: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
				{
					InlineManifest: &unstructured.Unstructured{
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
					InlineManifest: &unstructured.Unstructured{
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
					InlineManifest: &unstructured.Unstructured{
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
			want: []*ocv1ac.ClusterObjectSetPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseConfiguration)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
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
							InlineManifest: &unstructured.Unstructured{
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
							InlineManifest: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
				{
					InlineManifest: &unstructured.Unstructured{
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
					InlineManifest: &unstructured.Unstructured{
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
					InlineManifest: &unstructured.Unstructured{
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
					InlineManifest: &unstructured.Unstructured{
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
					InlineManifest: &unstructured.Unstructured{
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
					InlineManifest: &unstructured.Unstructured{
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
			want: []*ocv1ac.ClusterObjectSetPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseConfiguration)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
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
							InlineManifest: &unstructured.Unstructured{
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
							InlineManifest: &unstructured.Unstructured{
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
							InlineManifest: &unstructured.Unstructured{
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
					},
				},
				{
					Name: ptr.To(string(applier.PhaseDeploy)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
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
							InlineManifest: &unstructured.Unstructured{
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
			objs: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
				{
					InlineManifest: &unstructured.Unstructured{
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
					InlineManifest: &unstructured.Unstructured{
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
					InlineManifest: &unstructured.Unstructured{
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
			want: []*ocv1ac.ClusterObjectSetPhaseApplyConfiguration{
				{
					Name: ptr.To(string(applier.PhaseRoles)),
					Objects: []ocv1ac.ClusterObjectSetObjectApplyConfiguration{
						{
							InlineManifest: &unstructured.Unstructured{
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
							InlineManifest: &unstructured.Unstructured{
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
							InlineManifest: &unstructured.Unstructured{
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
