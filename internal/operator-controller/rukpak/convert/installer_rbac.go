package convert

import (
    "fmt"
    "github.com/operator-framework/operator-controller/internal/shared/util/filter"
    rbacv1 "k8s.io/api/rbac/v1"
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "k8s.io/utils/ptr"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "slices"
    "strings"
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
// the RBAC therein to another service account. Note: currently assumes objs have been created by convert.Convert.
func GenerateResourceManagerClusterRole(objs []client.Object) *rbacv1.ClusterRole {
    return ptr.To(newClusterRole(
        "",
        slices.Concat(
            // cluster scoped resource creation and management rules
            generatePolicyRules(filter.Filter(objs, isClusterScopedResource)),
            // controller rbac scope
            collectRBACResourcePolicyRules(filter.Filter(objs, filter.And(isGeneratedResource, isOfKind("ClusterRole")))),
        ),
    ))
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
func GenerateResourceManagerRoles(objs []client.Object) []*rbacv1.Role {
    return mapToSlice(filter.GroupBy(objs, namespaceName), generateRole)
}

func generateRole(namespace string, namespaceObjs []client.Object) *rbacv1.Role {
    return ptr.To(newRole(
        namespace,
        "",
        slices.Concat(
            // namespace scoped resource creation and management rules
            generatePolicyRules(namespaceObjs),
            // controller rbac scope
            collectRBACResourcePolicyRules(filter.Filter(namespaceObjs, filter.And(isOfKind("Role"), isGeneratedResource))),
        ),
    ))
}

func generatePolicyRules(objs []client.Object) []rbacv1.PolicyRule {
    return slices.Concat(mapToSlice(filter.GroupBy(objs, groupKind), func(gk schema.GroupKind, resources []client.Object) []rbacv1.PolicyRule {
        return []rbacv1.PolicyRule{
            newPolicyRule(gk, unnamedResourceVerbs),
            newPolicyRule(gk, namedResourceVerbs, filter.Map(resources, toResourceName)...),
        }
    })...)
}

func collectRBACResourcePolicyRules(objs []client.Object) []rbacv1.PolicyRule {
    return slices.Concat(filter.Map(objs, func(obj client.Object) []rbacv1.PolicyRule {
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

func isOfKind(kind string) filter.Predicate[client.Object] {
    return func(o client.Object) bool {
        return o.GetObjectKind().GroupVersionKind().Kind == kind
    }
}

func isGeneratedResource(o client.Object) bool {
    // TODO: this is a hack that abuses an internal implementation detail
    //       we should probably annotate the generated resources coming out of convert.Convert
    _, ok := o.(*unstructured.Unstructured)
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
