package generators_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/registryv1/generators"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

func Test_OptionsApplyToExecutesIgnoresNil(t *testing.T) {
	opts := []generators.ResourceCreatorOption{
		func(object client.Object) {
			object.SetAnnotations(util.MergeMaps(object.GetAnnotations(), map[string]string{"h": ""}))
		},
		nil,
		func(object client.Object) {
			object.SetAnnotations(util.MergeMaps(object.GetAnnotations(), map[string]string{"i": ""}))
		},
		nil,
	}

	require.Nil(t, generators.ResourceCreatorOptions(nil).ApplyTo(nil))
	require.Nil(t, generators.ResourceCreatorOptions([]generators.ResourceCreatorOption{}).ApplyTo(nil))

	obj := generators.ResourceCreatorOptions(opts).ApplyTo(&corev1.ConfigMap{})
	require.Equal(t, "hi", strings.Join(slices.Sorted(maps.Keys(obj.GetAnnotations())), ""))
}

func Test_CreateServiceAccount(t *testing.T) {
	svc := generators.CreateServiceAccountResource("my-sa", "my-namespace")
	require.NotNil(t, svc)
	require.Equal(t, "my-sa", svc.Name)
	require.Equal(t, "my-namespace", svc.Namespace)
}

func Test_CreateRole(t *testing.T) {
	role := generators.CreateRoleResource("my-role", "my-namespace")
	require.NotNil(t, role)
	require.Equal(t, "my-role", role.Name)
	require.Equal(t, "my-namespace", role.Namespace)
}

func Test_CreateRoleBinding(t *testing.T) {
	roleBinding := generators.CreateRoleBindingResource("my-role-binding", "my-namespace")
	require.NotNil(t, roleBinding)
	require.Equal(t, "my-role-binding", roleBinding.Name)
	require.Equal(t, "my-namespace", roleBinding.Namespace)
}

func Test_CreateClusterRole(t *testing.T) {
	clusterRole := generators.CreateClusterRoleResource("my-cluster-role")
	require.NotNil(t, clusterRole)
	require.Equal(t, "my-cluster-role", clusterRole.Name)
}

func Test_CreateClusterRoleBinding(t *testing.T) {
	clusterRoleBinding := generators.CreateClusterRoleBindingResource("my-cluster-role-binding")
	require.NotNil(t, clusterRoleBinding)
	require.Equal(t, "my-cluster-role-binding", clusterRoleBinding.Name)
}

func Test_CreateDeployment(t *testing.T) {
	deployment := generators.CreateDeploymentResource("my-deployment", "my-namespace")
	require.NotNil(t, deployment)
	require.Equal(t, "my-deployment", deployment.Name)
	require.Equal(t, "my-namespace", deployment.Namespace)
}

func Test_CreateService(t *testing.T) {
	svc := generators.CreateServiceResource("my-service", "my-namespace")
	require.NotNil(t, svc)
	require.Equal(t, "my-service", svc.Name)
	require.Equal(t, "my-namespace", svc.Namespace)
}

func Test_CreateValidatingWebhookConfiguration(t *testing.T) {
	wh := generators.CreateValidatingWebhookConfigurationResource("my-validating-webhook-configuration", "my-namespace")
	require.NotNil(t, wh)
	require.Equal(t, "my-validating-webhook-configuration", wh.Name)
	require.Equal(t, "my-namespace", wh.Namespace)
}

func Test_CreateMutatingWebhookConfiguration(t *testing.T) {
	wh := generators.CreateMutatingWebhookConfigurationResource("my-mutating-webhook-configuration", "my-namespace")
	require.NotNil(t, wh)
	require.Equal(t, "my-mutating-webhook-configuration", wh.Name)
	require.Equal(t, "my-namespace", wh.Namespace)
}

func Test_WithSubjects(t *testing.T) {
	for _, tc := range []struct {
		name     string
		subjects []rbacv1.Subject
	}{
		{
			name:     "empty",
			subjects: []rbacv1.Subject{},
		}, {
			name:     "nil",
			subjects: nil,
		}, {
			name: "single subject",
			subjects: []rbacv1.Subject{
				{
					APIGroup:  rbacv1.GroupName,
					Kind:      rbacv1.ServiceAccountKind,
					Name:      "my-sa",
					Namespace: "my-namespace",
				},
			},
		}, {
			name: "multiple subjects",
			subjects: []rbacv1.Subject{
				{
					APIGroup:  rbacv1.GroupName,
					Kind:      rbacv1.ServiceAccountKind,
					Name:      "my-sa",
					Namespace: "my-namespace",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roleBinding := generators.CreateRoleBindingResource("my-role", "my-namespace", generators.WithSubjects(tc.subjects...))
			require.NotNil(t, roleBinding)
			require.Equal(t, roleBinding.Subjects, tc.subjects)

			clusterRoleBinding := generators.CreateClusterRoleBindingResource("my-role", generators.WithSubjects(tc.subjects...))
			require.NotNil(t, clusterRoleBinding)
			require.Equal(t, clusterRoleBinding.Subjects, tc.subjects)
		})
	}
}

func Test_WithRules(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules []rbacv1.PolicyRule
	}{
		{
			name:  "empty",
			rules: []rbacv1.PolicyRule{},
		}, {
			name:  "nil",
			rules: nil,
		}, {
			name: "single subject",
			rules: []rbacv1.PolicyRule{
				{
					Verbs:     []string{"*"},
					APIGroups: []string{"*"},
					Resources: []string{"*"},
				},
			},
		}, {
			name: "multiple subjects",
			rules: []rbacv1.PolicyRule{
				{
					Verbs:         []string{"*"},
					APIGroups:     []string{"*"},
					Resources:     []string{"*"},
					ResourceNames: []string{"my-resource"},
				}, {
					Verbs:     []string{"get", "list", "watch"},
					APIGroups: []string{"appsv1"},
					Resources: []string{"deployments", "replicasets", "statefulsets"},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			role := generators.CreateRoleResource("my-role", "my-namespace", generators.WithRules(tc.rules...))
			require.NotNil(t, role)
			require.Equal(t, role.Rules, tc.rules)

			clusterRole := generators.CreateClusterRoleResource("my-role", generators.WithRules(tc.rules...))
			require.NotNil(t, clusterRole)
			require.Equal(t, clusterRole.Rules, tc.rules)
		})
	}
}

func Test_WithRoleRef(t *testing.T) {
	roleRef := rbacv1.RoleRef{
		APIGroup: rbacv1.GroupName,
		Kind:     "Role",
		Name:     "my-role",
	}

	roleBinding := generators.CreateRoleBindingResource("my-role-binding", "my-namespace", generators.WithRoleRef(roleRef))
	require.NotNil(t, roleBinding)
	require.Equal(t, roleRef, roleBinding.RoleRef)

	clusterRoleBinding := generators.CreateClusterRoleBindingResource("my-cluster-role-binding", generators.WithRoleRef(roleRef))
	require.NotNil(t, clusterRoleBinding)
	require.Equal(t, roleRef, clusterRoleBinding.RoleRef)
}

func Test_WithLabels(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels map[string]string
	}{
		{
			name:   "empty",
			labels: map[string]string{},
		}, {
			name:   "nil",
			labels: nil,
		}, {
			name: "not empty",
			labels: map[string]string{
				"foo": "bar",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dep := generators.CreateDeploymentResource("my-deployment", "my-namespace", generators.WithLabels(tc.labels))
			require.NotNil(t, dep)
			require.Equal(t, tc.labels, dep.Labels)
		})
	}
}

func Test_WithServiceSpec(t *testing.T) {
	svc := generators.CreateServiceResource("mysvc", "myns", generators.WithServiceSpec(corev1.ServiceSpec{
		ClusterIP: "1.2.3.4",
	}))
	require.NotNil(t, svc)
	require.Equal(t, corev1.ServiceSpec{
		ClusterIP: "1.2.3.4",
	}, svc.Spec)
}

func Test_WithValidatingWebhook(t *testing.T) {
	wh := generators.CreateValidatingWebhookConfigurationResource("mywh", "myns",
		generators.WithValidatingWebhooks(
			admissionregistrationv1.ValidatingWebhook{
				Name: "wh-one",
			},
			admissionregistrationv1.ValidatingWebhook{
				Name: "wh-two",
			},
		),
	)
	require.NotNil(t, wh)
	require.Equal(t, []admissionregistrationv1.ValidatingWebhook{
		{Name: "wh-one"},
		{Name: "wh-two"},
	}, wh.Webhooks)
}

func Test_WithMutatingWebhook(t *testing.T) {
	wh := generators.CreateMutatingWebhookConfigurationResource("mywh", "myns",
		generators.WithMutatingWebhooks(
			admissionregistrationv1.MutatingWebhook{
				Name: "wh-one",
			},
			admissionregistrationv1.MutatingWebhook{
				Name: "wh-two",
			},
		),
	)
	require.NotNil(t, wh)
	require.Equal(t, []admissionregistrationv1.MutatingWebhook{
		{Name: "wh-one"},
		{Name: "wh-two"},
	}, wh.Webhooks)
}

func Test_WithProxy(t *testing.T) {
	depSpec := appsv1.DeploymentSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name: "init-container",
						Env: []corev1.EnvVar{
							{
								Name:  "INIT_VAR",
								Value: "init-value",
							},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name: "c1",
						Env: []corev1.EnvVar{
							{
								Name:  "TEST",
								Value: "xxx",
							},
						},
					},
					{
						Name: "c2",
						Env: []corev1.EnvVar{
							{
								Name:  "APP_ENV",
								Value: "production",
							},
						},
					},
				},
			},
		},
	}

	depl := generators.CreateDeploymentResource(
		"test",
		"test-ns",
		generators.WithDeploymentSpec(depSpec),
		generators.WithProxy("http://proxy.example.com:8080", "https://proxy.example.com:8443", "localhost,.cluster.local"),
	)

	// Verify init container has proxy env vars
	expectedInitEnv := []corev1.EnvVar{
		{
			Name:  "INIT_VAR",
			Value: "init-value",
		},
		{
			Name:  "HTTP_PROXY",
			Value: "http://proxy.example.com:8080",
		},
		{
			Name:  "HTTPS_PROXY",
			Value: "https://proxy.example.com:8443",
		},
		{
			Name:  "NO_PROXY",
			Value: "localhost,.cluster.local",
		},
	}
	assert.Equal(t, expectedInitEnv, depl.Spec.Template.Spec.InitContainers[0].Env, "init container should have proxy env vars")

	// Verify first regular container has proxy env vars
	expectedC1Env := []corev1.EnvVar{
		{
			Name:  "TEST",
			Value: "xxx",
		},
		{
			Name:  "HTTP_PROXY",
			Value: "http://proxy.example.com:8080",
		},
		{
			Name:  "HTTPS_PROXY",
			Value: "https://proxy.example.com:8443",
		},
		{
			Name:  "NO_PROXY",
			Value: "localhost,.cluster.local",
		},
	}
	assert.Equal(t, expectedC1Env, depl.Spec.Template.Spec.Containers[0].Env, "container c1 should have proxy env vars")

	// Verify second regular container has proxy env vars
	expectedC2Env := []corev1.EnvVar{
		{
			Name:  "APP_ENV",
			Value: "production",
		},
		{
			Name:  "HTTP_PROXY",
			Value: "http://proxy.example.com:8080",
		},
		{
			Name:  "HTTPS_PROXY",
			Value: "https://proxy.example.com:8443",
		},
		{
			Name:  "NO_PROXY",
			Value: "localhost,.cluster.local",
		},
	}
	assert.Equal(t, expectedC2Env, depl.Spec.Template.Spec.Containers[1].Env, "container c2 should have proxy env vars")
}
