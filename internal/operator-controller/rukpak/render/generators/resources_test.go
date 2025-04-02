package generators_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render/generators"
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
