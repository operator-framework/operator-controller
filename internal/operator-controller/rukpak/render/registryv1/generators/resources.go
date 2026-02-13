package generators

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
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

// WithServiceSpec applies a service spec to a Service resource
func WithServiceSpec(serviceSpec corev1.ServiceSpec) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *corev1.Service:
			o.Spec = serviceSpec
		}
	}
}

// WithValidatingWebhooks applies validating webhooks to a ValidatingWebhookConfiguration resource
func WithValidatingWebhooks(webhooks ...admissionregistrationv1.ValidatingWebhook) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *admissionregistrationv1.ValidatingWebhookConfiguration:
			o.Webhooks = webhooks
		}
	}
}

// WithMutatingWebhooks applies mutating webhooks to a MutatingWebhookConfiguration resource
func WithMutatingWebhooks(webhooks ...admissionregistrationv1.MutatingWebhook) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *admissionregistrationv1.MutatingWebhookConfiguration:
			o.Webhooks = webhooks
		}
	}
}

// WithProxy applies HTTP proxy environment variables to Deployment resources.
// Proxy env vars are applied to both regular containers and init containers.
func WithProxy(httpProxy, httpsProxy, noProxy string) func(client.Object) {
	return func(obj client.Object) {
		switch o := obj.(type) {
		case *appsv1.Deployment:
			addProxyEnvVars(httpProxy, httpsProxy, noProxy, o.Spec.Template.Spec.Containers)
			addProxyEnvVars(httpProxy, httpsProxy, noProxy, o.Spec.Template.Spec.InitContainers)
		}
	}
}

func addProxyEnvVars(httpProxy, httpsProxy, noProxy string, containers []corev1.Container) {
	proxyEnvNames := []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"}

	for i := range containers {
		// First, remove any existing proxy env vars to ensure clean slate
		// This allows us to remove proxy vars when they're no longer configured
		var newEnv []corev1.EnvVar
		if len(containers[i].Env) > 0 {
			newEnv = make([]corev1.EnvVar, 0, len(containers[i].Env))
			for _, env := range containers[i].Env {
				isProxyVar := false
				for _, proxyName := range proxyEnvNames {
					if env.Name == proxyName {
						isProxyVar = true
						break
					}
				}
				if !isProxyVar {
					newEnv = append(newEnv, env)
				}
			}
		}
		containers[i].Env = newEnv

		// Then add the proxy env vars if they're configured
		if len(httpProxy) > 0 {
			containers[i].Env = append(containers[i].Env, corev1.EnvVar{
				Name:  "HTTP_PROXY",
				Value: httpProxy,
			})
		}
		if len(httpsProxy) > 0 {
			containers[i].Env = append(containers[i].Env, corev1.EnvVar{
				Name:  "HTTPS_PROXY",
				Value: httpsProxy,
			})
		}
		if len(noProxy) > 0 {
			containers[i].Env = append(containers[i].Env, corev1.EnvVar{
				Name:  "NO_PROXY",
				Value: noProxy,
			})
		}
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

// CreateValidatingWebhookConfigurationResource creates a ValidatingWebhookConfiguration resource with name 'name',
// namespace 'namespace', and applying any ValidatingWebhookConfiguration related options in opts
func CreateValidatingWebhookConfigurationResource(name string, namespace string, opts ...ResourceCreatorOption) *admissionregistrationv1.ValidatingWebhookConfiguration {
	return ResourceCreatorOptions(opts).ApplyTo(
		&admissionregistrationv1.ValidatingWebhookConfiguration{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ValidatingWebhookConfiguration",
				APIVersion: admissionregistrationv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		},
	).(*admissionregistrationv1.ValidatingWebhookConfiguration)
}

// CreateMutatingWebhookConfigurationResource creates a MutatingWebhookConfiguration resource with name 'name',
// namespace 'namespace', and applying any MutatingWebhookConfiguration related options in opts
func CreateMutatingWebhookConfigurationResource(name string, namespace string, opts ...ResourceCreatorOption) *admissionregistrationv1.MutatingWebhookConfiguration {
	return ResourceCreatorOptions(opts).ApplyTo(
		&admissionregistrationv1.MutatingWebhookConfiguration{
			TypeMeta: metav1.TypeMeta{
				Kind:       "MutatingWebhookConfiguration",
				APIVersion: admissionregistrationv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		},
	).(*admissionregistrationv1.MutatingWebhookConfiguration)
}

// CreateServiceResource creates a Service resource with name 'name', namespace 'namespace', and applying any Service related options in opts
func CreateServiceResource(name string, namespace string, opts ...ResourceCreatorOption) *corev1.Service {
	return ResourceCreatorOptions(opts).ApplyTo(&corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}).(*corev1.Service)
}
