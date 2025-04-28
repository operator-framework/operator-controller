package generators

import (
	"cmp"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	registrybundle "github.com/operator-framework/operator-registry/pkg/lib/bundle"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/render"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

// BundleCSVRBACResourceGenerator generates all ServiceAccounts, ClusterRoles, ClusterRoleBindings, Roles, RoleBindings
// defined in the RegistryV1 bundle's cluster service version (CSV)
var BundleCSVRBACResourceGenerator = render.ResourceGenerators{
	BundleCSVServiceAccountGenerator,
	BundleCSVPermissionsGenerator,
	BundleCSVClusterPermissionsGenerator,
}

// BundleCSVDeploymentGenerator generates all deployments defined in rv1's cluster service version (CSV). The generated
// resource aim to have parity with OLMv0 generated Deployment resources:
// - olm.targetNamespaces annotation is set with the opts.TargetNamespace value
// - the deployment spec's revision history limit is set to 1
// - merges csv annotations to the deployment template's annotations
func BundleCSVDeploymentGenerator(rv1 *render.RegistryV1, opts render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}
	objs := make([]client.Object, 0, len(rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs))
	for _, depSpec := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		// Add CSV annotations to template annotations
		// See https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/install/deployment.go#L142
		annotations := util.MergeMaps(rv1.CSV.Annotations, depSpec.Spec.Template.Annotations)

		// In OLMv0 CSVs are annotated with the OperatorGroup's .spec.targetNamespaces
		// See https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/operators/olm/operatorgroup.go#L279
		// When the CSVs annotations are copied to the deployment template's annotations, they bring with it this annotation
		annotations["olm.targetNamespaces"] = strings.Join(opts.TargetNamespaces, ",")
		depSpec.Spec.Template.Annotations = annotations

		// Hardcode the deployment with RevisionHistoryLimit=1 to maintain parity with OLMv0 behaviour.
		// See https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/install/deployment.go#L177-L180
		depSpec.Spec.RevisionHistoryLimit = ptr.To(int32(1))

		objs = append(objs,
			CreateDeploymentResource(
				depSpec.Name,
				opts.InstallNamespace,
				WithDeploymentSpec(depSpec.Spec),
				WithLabels(depSpec.Label),
			),
		)
	}
	return objs, nil
}

// BundleCSVPermissionsGenerator generates the Roles and RoleBindings based on bundle's cluster service version
// permission spec. If the bundle is being installed in AllNamespaces mode (opts.TargetNamespaces = [”])
// no resources will be generated as these permissions will be promoted to ClusterRole/Bunding(s)
func BundleCSVPermissionsGenerator(rv1 *render.RegistryV1, opts render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}

	// If we're in AllNamespaces mode permissions will be treated as clusterPermissions
	if len(opts.TargetNamespaces) == 1 && opts.TargetNamespaces[0] == "" {
		return nil, nil
	}

	permissions := rv1.CSV.Spec.InstallStrategy.StrategySpec.Permissions

	objs := make([]client.Object, 0, 2*len(opts.TargetNamespaces)*len(permissions))
	for _, ns := range opts.TargetNamespaces {
		for _, permission := range permissions {
			saName := saNameOrDefault(permission.ServiceAccountName)
			name, err := opts.UniqueNameGenerator(fmt.Sprintf("%s-%s", rv1.CSV.Name, saName), permission)
			if err != nil {
				return nil, err
			}

			objs = append(objs,
				CreateRoleResource(name, ns, WithRules(permission.Rules...)),
				CreateRoleBindingResource(
					name,
					ns,
					WithSubjects(rbacv1.Subject{Kind: "ServiceAccount", Namespace: opts.InstallNamespace, Name: saName}),
					WithRoleRef(rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name}),
				),
			)
		}
	}
	return objs, nil
}

// BundleCSVClusterPermissionsGenerator generates ClusterRoles and ClusterRoleBindings based on the bundle's
// cluster service version clusterPermission spec. If the bundle is being installed in AllNamespaces mode
// (opts.TargetNamespaces = [”]), the CSV's permission spec will be promoted to ClusterRole and ClusterRoleBinding
// resources. To keep parity with OLMv0, these will also include an extra rule to get, list, watch namespaces
// (see https://github.com/operator-framework/operator-lifecycle-manager/blob/dfd0b2bea85038d3c0d65348bc812d297f16b8d2/pkg/controller/operators/olm/operatorgroup.go#L539)
func BundleCSVClusterPermissionsGenerator(rv1 *render.RegistryV1, opts render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}
	clusterPermissions := rv1.CSV.Spec.InstallStrategy.StrategySpec.ClusterPermissions

	// If we're in AllNamespaces mode, promote the permissions to clusterPermissions
	if len(opts.TargetNamespaces) == 1 && opts.TargetNamespaces[0] == "" {
		for _, p := range rv1.CSV.Spec.InstallStrategy.StrategySpec.Permissions {
			p.Rules = append(p.Rules, rbacv1.PolicyRule{
				Verbs:     []string{"get", "list", "watch"},
				APIGroups: []string{corev1.GroupName},
				Resources: []string{"namespaces"},
			})
			clusterPermissions = append(clusterPermissions, p)
		}
	}

	objs := make([]client.Object, 0, 2*len(clusterPermissions))
	for _, permission := range clusterPermissions {
		saName := saNameOrDefault(permission.ServiceAccountName)
		name, err := opts.UniqueNameGenerator(fmt.Sprintf("%s-%s", rv1.CSV.Name, saName), permission)
		if err != nil {
			return nil, err
		}
		objs = append(objs,
			CreateClusterRoleResource(name, WithRules(permission.Rules...)),
			CreateClusterRoleBindingResource(
				name,
				WithSubjects(rbacv1.Subject{Kind: "ServiceAccount", Namespace: opts.InstallNamespace, Name: saName}),
				WithRoleRef(rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: name}),
			),
		)
	}
	return objs, nil
}

// BundleCSVServiceAccountGenerator generates ServiceAccount resources based on the bundle's cluster service version
// permission and clusterPermission spec. One ServiceAccount resource is created / referenced service account (i.e.
// if multiple permissions reference the same service account, only one resource will be generated).
// If a clusterPermission, or permission, references an empty (”) service account, this is considered to be the
// namespace 'default' service account. A resource for the namespace 'default' service account is not generated.
func BundleCSVServiceAccountGenerator(rv1 *render.RegistryV1, opts render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}
	allPermissions := append(
		rv1.CSV.Spec.InstallStrategy.StrategySpec.Permissions,
		rv1.CSV.Spec.InstallStrategy.StrategySpec.ClusterPermissions...,
	)

	serviceAccountNames := sets.Set[string]{}
	for _, permission := range allPermissions {
		serviceAccountNames.Insert(saNameOrDefault(permission.ServiceAccountName))
	}

	objs := make([]client.Object, 0, len(serviceAccountNames))
	for _, serviceAccountName := range serviceAccountNames.UnsortedList() {
		// no need to generate the default service account
		if serviceAccountName != "default" {
			objs = append(objs, CreateServiceAccountResource(serviceAccountName, opts.InstallNamespace))
		}
	}
	return objs, nil
}

// BundleCRDGenerator generates CustomResourceDefinition resources from the registry+v1 bundle
func BundleCRDGenerator(rv1 *render.RegistryV1, _ render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}
	objs := make([]client.Object, 0, len(rv1.CRDs))
	for _, crd := range rv1.CRDs {
		objs = append(objs, crd.DeepCopy())
	}
	return objs, nil
}

// BundleAdditionalResourcesGenerator generates resources for the additional resources included in the
// bundle. If the bundle resource is namespace scoped, its namespace will be set to the value of opts.InstallNamespace.
func BundleAdditionalResourcesGenerator(rv1 *render.RegistryV1, opts render.Options) ([]client.Object, error) {
	if rv1 == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}
	objs := make([]client.Object, 0, len(rv1.Others))
	for _, res := range rv1.Others {
		supported, namespaced := registrybundle.IsSupported(res.GetKind())
		if !supported {
			return nil, fmt.Errorf("bundle contains unsupported resource: Name: %v, Kind: %v", res.GetName(), res.GetKind())
		}

		obj := res.DeepCopy()
		if namespaced {
			obj.SetNamespace(opts.InstallNamespace)
		}

		objs = append(objs, obj)
	}
	return objs, nil
}

func saNameOrDefault(saName string) string {
	return cmp.Or(saName, "default")
}
