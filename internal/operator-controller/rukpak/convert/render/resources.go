package render

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ResourceGenerationOption = func(client.Object)
type ResourceGenerationOptions []ResourceGenerationOption

func (r ResourceGenerationOptions) ApplyTo(obj client.Object) client.Object {
	if obj == nil {
		return nil
	}
	for _, opt := range r {
		if opt != nil {
			opt(obj)
		}
	}
	return obj
}

func WithSubjects(subjects ...rbacv1.Subject) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *rbacv1.RoleBinding:
			o.Subjects = subjects
		case *rbacv1.ClusterRoleBinding:
			o.Subjects = subjects
		}
	}
}

func WithRoleRef(roleRef rbacv1.RoleRef) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *rbacv1.RoleBinding:
			o.RoleRef = roleRef
		case *rbacv1.ClusterRoleBinding:
			o.RoleRef = roleRef
		}
	}
}

func WithRules(rules ...rbacv1.PolicyRule) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *rbacv1.Role:
			o.Rules = rules
		case *rbacv1.ClusterRole:
			o.Rules = rules
		}
	}
}

func WithDeploymentSpec(depSpec appsv1.DeploymentSpec) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *appsv1.Deployment:
			o.Spec = depSpec
		}
	}
}

func WithLabels(labels map[string]string) func(client.Object) {
	return func(obj client.Object) {
		obj.SetLabels(labels)
	}
}

func GenerateServiceAccountResource(name string, namespace string, opts ...ResourceGenerationOption) *corev1.ServiceAccount {
	return ResourceGenerationOptions(opts).ApplyTo(
		&corev1.ServiceAccount{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ServiceAccount",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
		},
	).(*corev1.ServiceAccount)
}

func GenerateRoleResource(name string, namespace string, opts ...ResourceGenerationOption) *rbacv1.Role {
	return ResourceGenerationOptions(opts).ApplyTo(
		&rbacv1.Role{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Role",
				APIVersion: rbacv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
		},
	).(*rbacv1.Role)
}

func GenerateClusterRoleResource(name string, opts ...ResourceGenerationOption) *rbacv1.ClusterRole {
	return ResourceGenerationOptions(opts).ApplyTo(
		&rbacv1.ClusterRole{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ClusterRole",
				APIVersion: rbacv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
	).(*rbacv1.ClusterRole)
}

func GenerateClusterRoleBindingResource(name string, opts ...ResourceGenerationOption) *rbacv1.ClusterRoleBinding {
	return ResourceGenerationOptions(opts).ApplyTo(
		&rbacv1.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ClusterRoleBinding",
				APIVersion: rbacv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		},
	).(*rbacv1.ClusterRoleBinding)
}

func GenerateRoleBindingResource(name string, namespace string, opts ...ResourceGenerationOption) *rbacv1.RoleBinding {
	return ResourceGenerationOptions(opts).ApplyTo(
		&rbacv1.RoleBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RoleBinding",
				APIVersion: rbacv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
		},
	).(*rbacv1.RoleBinding)
}

func GenerateDeploymentResource(name string, namespace string, opts ...ResourceGenerationOption) *appsv1.Deployment {
	return ResourceGenerationOptions(opts).ApplyTo(
		&appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Deployment",
				APIVersion: appsv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
		},
	).(*appsv1.Deployment)
}
