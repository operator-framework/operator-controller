package convert_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
)

func Test_GenerateResourceManagerClusterRolePerms_GeneratesRBACSuccessfully(t *testing.T) {
	objs := []client.Object{
		// ClusterRole created by convert.Convert
		&rbacv1.ClusterRole{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ClusterRole",
				APIVersion: rbacv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "operator-controller-cluster-perms",
				Annotations: map[string]string{
					convert.AnnotationRegistryV1GeneratedManifest: "",
				},
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
					Verbs:     []string{"get", "list", "watch"},
				}, {
					APIGroups: []string{"apps"},
					Resources: []string{"deployments"},
					Verbs:     []string{"get", "list", "watch"},
				},
			},
		},
		// Some CRD
		&apiextensions.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CustomResourceDefinition",
				APIVersion: apiextensions.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "operator-controller-crd",
			},
			Spec: apiextensions.CustomResourceDefinitionSpec{
				Group: "some.operator.domain",
				Names: apiextensions.CustomResourceDefinitionNames{
					Plural:   "operatorresources",
					Kind:     "OperatorResource",
					ListKind: "OperatorResourceList",
					Singular: "OperatorResource",
				},
				Versions: []apiextensions.CustomResourceDefinitionVersion{
					{
						Name:   "v1alpha1",
						Served: true,
					},
				},
			},
		},
		// Some Namespaced Resource
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "operator-controller-config",
			},
			Data: map[string]string{
				"some": "data",
			},
		},
	}

	clusterRolePerms := convert.GenerateResourceManagerClusterRolePerms(objs)
	require.ElementsMatch(t, []rbacv1.PolicyRule{
		// Aggregates operator-controller ClusterRole rules
		{
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
			Verbs:     []string{"get", "list", "watch"},
		}, {
			APIGroups: []string{"apps"},
			Resources: []string{"deployments"},
			Verbs:     []string{"get", "list", "watch"},
		},
		// Adds cluster-scoped resource management rules
		{
			APIGroups: []string{"rbac.authorization.k8s.io"},
			Resources: []string{"clusterroles"},
			Verbs:     []string{"create", "list", "watch"},
		}, {
			APIGroups:     []string{"rbac.authorization.k8s.io"},
			Resources:     []string{"clusterroles"},
			Verbs:         []string{"get", "update", "patch", "delete"},
			ResourceNames: []string{"operator-controller-cluster-perms"},
		}, {
			APIGroups: []string{"apiextensions.k8s.io"},
			Resources: []string{"customresourcedefinitions"},
			Verbs:     []string{"create", "list", "watch"},
		}, {
			APIGroups:     []string{"apiextensions.k8s.io"},
			Resources:     []string{"customresourcedefinitions"},
			Verbs:         []string{"get", "update", "patch", "delete"},
			ResourceNames: []string{"operator-controller-crd"},
		},
		// Nothing to be said about namespaced resources
	}, clusterRolePerms)
}

func Test_GenerateResourceManagerRolePerms_GeneratesRBACSuccessfully(t *testing.T) {
	objs := []client.Object{
		// ClusterRole generated by convert.Convert - should be ignored
		&rbacv1.ClusterRole{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ClusterRole",
				APIVersion: rbacv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "operator-controller-namespace-perms",
				Annotations: map[string]string{
					convert.AnnotationRegistryV1GeneratedManifest: "",
				},
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
					Verbs:     []string{"get", "list", "watch"},
				}, {
					APIGroups: []string{"apps"},
					Resources: []string{"deployments"},
					Verbs:     []string{"get", "list", "watch"},
				},
			},
		},
		// Some cluster-scoped resources - should be ignored
		&apiextensions.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CustomResourceDefinition",
				APIVersion: apiextensions.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "operator-controller-crd",
			},
			Spec: apiextensions.CustomResourceDefinitionSpec{
				Group: "some.operator.domain",
				Names: apiextensions.CustomResourceDefinitionNames{
					Plural:   "operatorresources",
					Kind:     "OperatorResource",
					ListKind: "OperatorResourceList",
					Singular: "OperatorResource",
				},
				Versions: []apiextensions.CustomResourceDefinitionVersion{
					{
						Name:   "v1alpha1",
						Served: true,
					},
				},
			},
		},
		// Some Namespaced Resource
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "operator-controller-config",
				Namespace: "install-namespace",
			},
			Data: map[string]string{
				"some": "data",
			},
		},
		// Some namespaces resource in a different namespace
		&corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "some-service",
				Namespace: "another-namespace",
			},
		},
		// Some convert.Convert generated Role - perms should be aggregated
		&rbacv1.Role{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Role",
				APIVersion: rbacv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "operator-controller-perms",
				Namespace: "install-namespace",
				Annotations: map[string]string{
					convert.AnnotationRegistryV1GeneratedManifest: "",
				},
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"configmaps"},
					Verbs:     []string{"get", "list", "watch"},
				}, {
					APIGroups: []string{"apps"},
					Resources: []string{"deployments"},
					Verbs:     []string{"get", "list", "watch"},
				},
			},
		},
	}

	namespaceRolePerms := convert.GenerateResourceManagerRolePerms(objs)
	expected := map[string][]rbacv1.PolicyRule{
		"install-namespace": {
			// Aggregates operator-controller ClusterRole rules
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get", "list", "watch"},
			}, {
				APIGroups: []string{"apps"},
				Resources: []string{"deployments"},
				Verbs:     []string{"get", "list", "watch"},
			},
			// Adds cluster-scoped resource management rules
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"create", "list", "watch"},
			}, {
				APIGroups:     []string{""},
				Resources:     []string{"configmaps"},
				Verbs:         []string{"get", "update", "patch", "delete"},
				ResourceNames: []string{"operator-controller-config"},
			},
			{
				APIGroups: []string{"rbac.authorization.k8s.io"},
				Resources: []string{"roles"},
				Verbs:     []string{"create", "list", "watch"},
			}, {
				APIGroups:     []string{"rbac.authorization.k8s.io"},
				Resources:     []string{"roles"},
				Verbs:         []string{"get", "update", "patch", "delete"},
				ResourceNames: []string{"operator-controller-perms"},
			},
		},
		"another-namespace": {
			{
				APIGroups: []string{""},
				Resources: []string{"services"},
				Verbs:     []string{"create", "list", "watch"},
			}, {
				APIGroups:     []string{""},
				Resources:     []string{"services"},
				Verbs:         []string{"get", "update", "patch", "delete"},
				ResourceNames: []string{"some-service"},
			},
		},
	}

	for namespace, perms := range namespaceRolePerms {
		require.ElementsMatch(t, perms, expected[namespace])
	}
}

func Test_GenerateClusterExtensionFinalizerPolicyRule(t *testing.T) {
	rule := convert.GenerateClusterExtensionFinalizerPolicyRule("someext")
	require.Equal(t, rbacv1.PolicyRule{
		APIGroups:     []string{"olm.operatorframework.io"},
		Resources:     []string{"clusterextensions/finalizers"},
		Verbs:         []string{"update"},
		ResourceNames: []string{"someext"},
	}, rule)
}
