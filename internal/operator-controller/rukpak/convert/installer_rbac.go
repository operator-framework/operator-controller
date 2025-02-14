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

func GenerateInstallerRBAC(objs []client.Object, extensionName string, installNamespace string, watchNamespace string) []client.Object {
    generatedObjs := getGeneratedObjs(objs)

    var (
        serviceAccountName     = extensionName + "-installer"
        clusterRoleName        = extensionName + "-installer-clusterrole"
        clusterRoleBindingName = extensionName + "-installer-clusterrole-binding"
        roleName               = extensionName + "-installer-role"
        roleBindingName        = extensionName + "-installer-role-binding"
    )

    rbacManifests := []client.Object{
        // installer service account
        ptr.To(newServiceAccount(
            installNamespace,
            serviceAccountName,
        )),

        // cluster scoped resources
        ptr.To(newClusterRole(
            clusterRoleName,
            slices.Concat(
                // finalizer rule
                []rbacv1.PolicyRule{newClusterExtensionFinalizerPolicyRule(extensionName)},
                // cluster scoped resource creation and management rules
                generatePolicyRules(filter.Filter(objs, isClusterScopedResource)),
                // controller rbac scope
                collectRBACResourcePolicyRules(filter.Filter(generatedObjs, isClusterRole)),
            ),
        )),
        ptr.To(newClusterRoleBinding(
            clusterRoleBindingName,
            clusterRoleName,
            installNamespace,
            serviceAccountName,
        )),

        // namespace scoped install namespace resources
        ptr.To(newRole(
            installNamespace,
            roleName,
            slices.Concat(
                // namespace scoped resource creation and management rules
                generatePolicyRules(filter.Filter(objs, filter.And(isNamespacedResource, inNamespace(installNamespace)))),
                // controller rbac scope
                collectRBACResourcePolicyRules(filter.Filter(generatedObjs, filter.And(isRole, inNamespace(installNamespace)))),
            ),
        )),
        ptr.To(newRoleBinding(
            installNamespace,
            roleBindingName,
            roleName,
            installNamespace,
            serviceAccountName,
        )),

        // namespace scoped watch namespace resources
        ptr.To(newRole(
            watchNamespace,
            roleName,
            slices.Concat(
                // namespace scoped resource creation and management rules
                generatePolicyRules(filter.Filter(objs, filter.And(isNamespacedResource, inNamespace(watchNamespace)))),
                // controller rbac scope
                collectRBACResourcePolicyRules(filter.Filter(generatedObjs, filter.And(isRole, inNamespace(watchNamespace)))),
            ),
        )),
        ptr.To(newRoleBinding(
            watchNamespace,
            roleBindingName,
            roleName,
            installNamespace,
            serviceAccountName,
        )),
    }

    // remove any cluster/role(s) without any defined rules and pair cluster/role add manifests
    return slices.DeleteFunc(rbacManifests, isNoRules)
}

func isNoRules(object client.Object) bool {
    switch obj := object.(type) {
    case *rbacv1.ClusterRole:
        return len(obj.Rules) == 0
    case *rbacv1.Role:
        return len(obj.Rules) == 0
    }
    return false
}

func getGeneratedObjs(plainObjs []client.Object) []client.Object {
    return filter.Filter(plainObjs, func(obj client.Object) bool {
        // this is a hack that abuses an internal implementation detail
        // we should probably annotate the generated resources coming out of convert.Convert
        _, ok := obj.(*unstructured.Unstructured)
        return ok
    })
}

var isNamespacedResource filter.Predicate[client.Object] = func(o client.Object) bool {
    return slices.Contains(namespaceScopedResources, o.GetObjectKind().GroupVersionKind().Kind)
}

var isClusterScopedResource filter.Predicate[client.Object] = func(o client.Object) bool {
    return slices.Contains(clusterScopedResources, o.GetObjectKind().GroupVersionKind().Kind)
}

var isClusterRole = isOfKind("ClusterRole")
var isRole = isOfKind("Role")

func isOfKind(kind string) filter.Predicate[client.Object] {
    return func(o client.Object) bool {
        return o.GetObjectKind().GroupVersionKind().Kind == kind
    }
}

func inNamespace(namespace string) filter.Predicate[client.Object] {
    return func(o client.Object) bool {
        return o.GetNamespace() == namespace
    }
}

func generatePolicyRules(objs []client.Object) []rbacv1.PolicyRule {
    resourceNameMap := groupResourceNamesByGroupKind(objs)
    policyRules := make([]rbacv1.PolicyRule, 0, 2*len(resourceNameMap))
    for groupKind, resourceNames := range resourceNameMap {
        policyRules = append(policyRules, []rbacv1.PolicyRule{
            newPolicyRule(groupKind, unnamedResourceVerbs),
            newPolicyRule(groupKind, namedResourceVerbs, resourceNames...),
        }...)
    }
    return policyRules
}

func collectRBACResourcePolicyRules(objs []client.Object) []rbacv1.PolicyRule {
    var policyRules []rbacv1.PolicyRule
    for _, obj := range objs {
        if cr, ok := obj.(*rbacv1.ClusterRole); ok {
            policyRules = append(policyRules, cr.Rules...)
        } else if r, ok := obj.(*rbacv1.Role); ok {
            policyRules = append(policyRules, r.Rules...)
        } else {
            panic(fmt.Sprintf("unexpected type %T", obj))
        }
    }
    return policyRules
}

func newClusterExtensionFinalizerPolicyRule(clusterExtensionName string) rbacv1.PolicyRule {
    return rbacv1.PolicyRule{
        APIGroups:     []string{"olm.operatorframework.io"},
        Resources:     []string{"clusterextensions/finalizers"},
        Verbs:         []string{"update"},
        ResourceNames: []string{clusterExtensionName},
    }
}

func groupResourceNamesByGroupKind(objs []client.Object) map[schema.GroupKind][]string {
    resourceNames := map[schema.GroupKind][]string{}
    for _, obj := range objs {
        key := obj.GetObjectKind().GroupVersionKind().GroupKind()
        if _, ok := resourceNames[key]; !ok {
            resourceNames[key] = []string{}
        }
        resourceNames[key] = append(resourceNames[key], obj.GetName())
    }
    return resourceNames
}

func newPolicyRule(groupKind schema.GroupKind, verbs []string, resourceNames ...string) rbacv1.PolicyRule {
    return rbacv1.PolicyRule{
        APIGroups:     []string{groupKind.Group},
        Resources:     []string{fmt.Sprintf("%ss", strings.ToLower(groupKind.Kind))},
        Verbs:         verbs,
        ResourceNames: resourceNames,
    }
}
