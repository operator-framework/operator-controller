package convert

import (
	"fmt"
	"slices"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	slicesutil "github.com/operator-framework/operator-controller/internal/shared/util/filter"
)

var (
	unnamedResourceVerbs = []string{"create", "list", "watch"}
	namedResourceVerbs   = []string{"get", "update", "patch", "delete"}

	// clusterScopedResources is a slice of registry+v1 bundle supported cluster scoped resource kinds
	clusterScopedResources = []string{
		"ClusterRole",
		"ClusterRoleBinding",
		"PriorityClass",
		"ConsoleYAMLSample",
		"ConsoleQuickStart",
		"ConsoleCLIDownload",
		"ConsoleLink",
		"CustomResourceDefinition",
	}

	// clusterScopedResources is a slice of registry+v1 bundle supported namespace scoped resource kinds
	namespaceScopedResources = []string{
		"Secret",
		"ConfigMap",
		"ServiceAccount",
		"Service",
		"Role",
		"RoleBinding",
		"PrometheusRule",
		"ServiceMonitor",
		"PodDisruptionBudget",
		"VerticalPodAutoscaler",
		"Deployment",
	}
)

// GenerateResourceManagerClusterRole generates a ClusterRole with permissions to manage objs resources. The
// permissions also aggregate any permissions from any ClusterRoles in objs allowing the holder to also assign
// the RBAC therein to another service account. Note: assumes objs have been created by convert.Convert.
// The returned ClusterRole will not have set .metadata.name
func GenerateResourceManagerClusterRole(objs []client.Object) *rbacv1.ClusterRole {
	rules := slices.Concat(
		// cluster scoped resource creation and management rules
		generatePolicyRules(slicesutil.Filter(objs, isClusterScopedResource)),
		// controller rbac scope
		collectRBACResourcePolicyRules(slicesutil.Filter(objs, slicesutil.And(isGeneratedResource, isOfKind("ClusterRole")))),
	)
	if len(rules) == 0 {
		return nil
	}
	return ptr.To(newClusterRole("", rules))
}

// GenerateClusterExtensionFinalizerPolicyRule generates a policy rule that allows the holder to update
// finalizer for a ClusterExtension with clusterExtensionName.
func GenerateClusterExtensionFinalizerPolicyRule(clusterExtensionName string) rbacv1.PolicyRule {
	return rbacv1.PolicyRule{
		APIGroups:     []string{"olm.operatorframework.io"},
		Resources:     []string{"clusterextensions/finalizers"},
		Verbs:         []string{"update"},
		ResourceNames: []string{clusterExtensionName},
	}
}

// GenerateResourceManagerRoles generates one or more Roles with permissions to manage objs resources in their
// namespaces. The permissions also include any permissions defined in any Roles in objs within the namespace, allowing
// the holder to also assign the RBAC therein to another service account.
// Note: currently assumes objs have been created by convert.Convert.
// The returned Roles will not have set .metadata.name
func GenerateResourceManagerRoles(objs []client.Object) []*rbacv1.Role {
	return mapToSlice(slicesutil.GroupBy(slicesutil.Filter(objs, isNamespaceScopedResource), namespaceName), generateRole)
}

func generateRole(namespace string, namespaceObjs []client.Object) *rbacv1.Role {
	return ptr.To(newRole(
		namespace,
		"",
		slices.Concat(
			// namespace scoped resource creation and management rules
			generatePolicyRules(namespaceObjs),
			// controller rbac scope
			collectRBACResourcePolicyRules(slicesutil.Filter(namespaceObjs, slicesutil.And(isOfKind("Role"), isGeneratedResource))),
		),
	))
}

func generatePolicyRules(objs []client.Object) []rbacv1.PolicyRule {
	return slices.Concat(
		mapToSlice(slicesutil.GroupBy(objs, groupKind), func(gk schema.GroupKind, resources []client.Object) []rbacv1.PolicyRule {
			return []rbacv1.PolicyRule{
				newPolicyRule(gk, unnamedResourceVerbs),
				newPolicyRule(gk, namedResourceVerbs, slicesutil.Map(resources, toResourceName)...),
			}
		})...,
	)
}

func collectRBACResourcePolicyRules(objs []client.Object) []rbacv1.PolicyRule {
	return slices.Concat(slicesutil.Map(objs, func(obj client.Object) []rbacv1.PolicyRule {
		if cr, ok := obj.(*rbacv1.ClusterRole); ok {
			return cr.Rules
		} else if r, ok := obj.(*rbacv1.Role); ok {
			return r.Rules
		} else {
			panic(fmt.Sprintf("unexpected type %T", obj))
		}
	})...)
}

func newPolicyRule(groupKind schema.GroupKind, verbs []string, resourceNames ...string) rbacv1.PolicyRule {
	return rbacv1.PolicyRule{
		APIGroups:     []string{groupKind.Group},
		Resources:     []string{fmt.Sprintf("%ss", strings.ToLower(groupKind.Kind))},
		Verbs:         verbs,
		ResourceNames: resourceNames,
	}
}

func mapToSlice[K comparable, V any, R any](m map[K]V, fn func(k K, v V) R) []R {
	out := make([]R, 0, len(m))
	for k, v := range m {
		out = append(out, fn(k, v))
	}
	return out
}

func isClusterScopedResource(o client.Object) bool {
	return slices.Contains(clusterScopedResources, o.GetObjectKind().GroupVersionKind().Kind)
}

func isNamespaceScopedResource(o client.Object) bool {
	return slices.Contains(namespaceScopedResources, o.GetObjectKind().GroupVersionKind().Kind)
}

func isOfKind(kind string) slicesutil.Predicate[client.Object] {
	return func(o client.Object) bool {
		return o.GetObjectKind().GroupVersionKind().Kind == kind
	}
}

func isGeneratedResource(o client.Object) bool {
	annotations := o.GetAnnotations()
	_, ok := annotations[AnnotationRegistryV1GeneratedManifest]
	return ok
}

func groupKind(obj client.Object) schema.GroupKind {
	return obj.GetObjectKind().GroupVersionKind().GroupKind()
}

func namespaceName(obj client.Object) string {
	return obj.GetNamespace()
}

func toResourceName(o client.Object) string {
	return o.GetName()
}
