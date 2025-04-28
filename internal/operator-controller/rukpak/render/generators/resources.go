package generators

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ResourceCreatorOption = func(client.Object)
type ResourceCreatorOptions []ResourceCreatorOption

func (r ResourceCreatorOptions) ApplyTo(obj client.Object) client.Object {
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

// WithSubjects applies rbac subjects to ClusterRoleBinding and RoleBinding resources
func WithSubjects(subjects ...rbacv1.Subject) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *rbacv1.RoleBinding:
			o.Subjects = subjects
		case *rbacv1.ClusterRoleBinding:
			o.Subjects = subjects
		default:
			panic("unknown object type")
		}
	}
}

// WithRoleRef applies rbac RoleRef to ClusterRoleBinding and RoleBinding resources
func WithRoleRef(roleRef rbacv1.RoleRef) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *rbacv1.RoleBinding:
			o.RoleRef = roleRef
		case *rbacv1.ClusterRoleBinding:
			o.RoleRef = roleRef
		default:
			panic("unknown object type")
		}
	}
}

// WithRules applies rbac PolicyRules to Role and ClusterRole resources
func WithRules(rules ...rbacv1.PolicyRule) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *rbacv1.Role:
			o.Rules = rules
		case *rbacv1.ClusterRole:
			o.Rules = rules
		default:
			panic("unknown object type")
		}
	}
}

// WithDeploymentSpec applies a DeploymentSpec to Deployment resources
func WithDeploymentSpec(depSpec appsv1.DeploymentSpec) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *appsv1.Deployment:
			o.Spec = depSpec
		default:
			panic("unknown object type")
		}
	}
}

// WithLabels applies labels to the metadata of any resource
func WithLabels(labels map[string]string) func(client.Object) {
	return func(obj client.Object) {
		obj.SetLabels(labels)
	}
}

// CreateServiceAccountResource creates a ServiceAccount resource with name 'name', namespace 'namespace', and applying
// any ServiceAccount related options in opts
func CreateServiceAccountResource(name string, namespace string, opts ...ResourceCreatorOption) *corev1.ServiceAccount {
	return ResourceCreatorOptions(opts).ApplyTo(
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

// CreateRoleResource creates a Role resource with name 'name' and namespace 'namespace' and applying any
// Role related options in opts
func CreateRoleResource(name string, namespace string, opts ...ResourceCreatorOption) *rbacv1.Role {
	return ResourceCreatorOptions(opts).ApplyTo(
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

// CreateClusterRoleResource creates a ClusterRole resource with name 'name' and applying any
// ClusterRole related options in opts
func CreateClusterRoleResource(name string, opts ...ResourceCreatorOption) *rbacv1.ClusterRole {
	return ResourceCreatorOptions(opts).ApplyTo(
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

// CreateClusterRoleBindingResource creates a ClusterRoleBinding resource with name 'name' and applying any
// ClusterRoleBinding related options in opts
func CreateClusterRoleBindingResource(name string, opts ...ResourceCreatorOption) *rbacv1.ClusterRoleBinding {
	return ResourceCreatorOptions(opts).ApplyTo(
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

// CreateRoleBindingResource creates a RoleBinding resource with name 'name', namespace 'namespace', and applying any
// RoleBinding related options in opts
func CreateRoleBindingResource(name string, namespace string, opts ...ResourceCreatorOption) *rbacv1.RoleBinding {
	return ResourceCreatorOptions(opts).ApplyTo(
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

// CreateDeploymentResource creates a Deployment resource with name 'name', namespace 'namespace', and applying any
// Deployment related options in opts
func CreateDeploymentResource(name string, namespace string, opts ...ResourceCreatorOption) *appsv1.Deployment {
	return ResourceCreatorOptions(opts).ApplyTo(
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
