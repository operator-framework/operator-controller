package render_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

func Test_OptionsApplyToExecutesIgnoresNil(t *testing.T) {
	opts := []render.ResourceGenerationOption{
		func(object client.Object) {
			object.SetAnnotations(util.MergeMaps(object.GetAnnotations(), map[string]string{"h": ""}))
		},
		nil,
		func(object client.Object) {
			object.SetAnnotations(util.MergeMaps(object.GetAnnotations(), map[string]string{"i": ""}))
		},
		nil,
	}

	require.Nil(t, render.ResourceGenerationOptions(nil).ApplyTo(nil))
	require.Nil(t, render.ResourceGenerationOptions([]render.ResourceGenerationOption{}).ApplyTo(nil))

	obj := render.ResourceGenerationOptions(opts).ApplyTo(&corev1.ConfigMap{})
	require.Equal(t, "hi", strings.Join(slices.Sorted(maps.Keys(obj.GetAnnotations())), ""))
}

func Test_GenerateServiceAccount(t *testing.T) {
	svc := render.GenerateServiceAccountResource("my-sa", "my-namespace")
	require.NotNil(t, svc)
	require.Equal(t, "my-sa", svc.Name)
	require.Equal(t, "my-namespace", svc.Namespace)
}

func Test_GenerateRole(t *testing.T) {
	role := render.GenerateRoleResource("my-role", "my-namespace")
	require.NotNil(t, role)
	require.Equal(t, "my-role", role.Name)
	require.Equal(t, "my-namespace", role.Namespace)
}

func Test_GenerateRoleBinding(t *testing.T) {
	roleBinding := render.GenerateRoleBindingResource("my-role-binding", "my-namespace")
	require.NotNil(t, roleBinding)
	require.Equal(t, "my-role-binding", roleBinding.Name)
	require.Equal(t, "my-namespace", roleBinding.Namespace)
}

func Test_GenerateClusterRole(t *testing.T) {
	clusterRole := render.GenerateClusterRoleResource("my-cluster-role")
	require.NotNil(t, clusterRole)
	require.Equal(t, "my-cluster-role", clusterRole.Name)
}

func Test_GenerateClusterRoleBinding(t *testing.T) {
	clusterRoleBinding := render.GenerateClusterRoleBindingResource("my-cluster-role-binding")
	require.NotNil(t, clusterRoleBinding)
	require.Equal(t, "my-cluster-role-binding", clusterRoleBinding.Name)
}

func Test_GenerateDeployment(t *testing.T) {
	deployment := render.GenerateDeploymentResource("my-deployment", "my-namespace")
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
			roleBinding := render.GenerateRoleBindingResource("my-role", "my-namespace", render.WithSubjects(tc.subjects...))
			require.NotNil(t, roleBinding)
			require.Equal(t, roleBinding.Subjects, tc.subjects)

			clusterRoleBinding := render.GenerateClusterRoleBindingResource("my-role", render.WithSubjects(tc.subjects...))
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
			role := render.GenerateRoleResource("my-role", "my-namespace", render.WithRules(tc.rules...))
			require.NotNil(t, role)
			require.Equal(t, role.Rules, tc.rules)

			clusterRole := render.GenerateClusterRoleResource("my-role", render.WithRules(tc.rules...))
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

	roleBinding := render.GenerateRoleBindingResource("my-role-binding", "my-namespace", render.WithRoleRef(roleRef))
	require.NotNil(t, roleBinding)
	require.Equal(t, roleRef, roleBinding.RoleRef)

	clusterRoleBinding := render.GenerateClusterRoleBindingResource("my-cluster-role-binding", render.WithRoleRef(roleRef))
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
			dep := render.GenerateDeploymentResource("my-deployment", "my-namespace", render.WithLabels(tc.labels))
			require.NotNil(t, dep)
			require.Equal(t, tc.labels, dep.Labels)
		})
	}
}
