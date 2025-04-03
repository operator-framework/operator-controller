package render

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	registrybundle "github.com/operator-framework/operator-registry/pkg/lib/bundle"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/util"
)

type UniqueNameGenerator func(string, interface{}) (string, error)

type Options struct {
	InstallNamespace    string
	TargetNamespaces    []string
	UniqueNameGenerator UniqueNameGenerator
	CertificateProvider CertificateProvider
}

type ResourceGenerator func(rv1 *convert.RegistryV1, opts Options) ([]client.Object, error)

func (g ResourceGenerator) GenerateResources(rv1 *convert.RegistryV1, opts Options) ([]client.Object, error) {
	return g(rv1, opts)
}

func ChainedResourceGenerator(resourceGenerators ...ResourceGenerator) ResourceGenerator {
	return func(rv1 *convert.RegistryV1, opts Options) ([]client.Object, error) {
		//nolint:prealloc
		var renderedObjects []client.Object
		for _, generator := range resourceGenerators {
			objs, err := generator(rv1, opts)
			if err != nil {
				return nil, err
			}
			renderedObjects = append(renderedObjects, objs...)
		}
		return renderedObjects, nil
	}
}

func BundleDeploymentGenerator(rv1 *convert.RegistryV1, opts Options) ([]client.Object, error) {
	//nolint:prealloc
	var objs []client.Object
	for _, depSpec := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		annotations := util.MergeMaps(rv1.CSV.Annotations, depSpec.Spec.Template.Annotations)
		annotations["olm.targetNamespaces"] = strings.Join(opts.TargetNamespaces, ",")
		depSpec.Spec.Template.Annotations = annotations

		// Hardcode the deployment with RevisionHistoryLimit=1 (something OLMv0 does, not sure why)
		depSpec.Spec.RevisionHistoryLimit = ptr.To(int32(1))

		objs = append(objs,
			GenerateDeploymentResource(
				depSpec.Name,
				opts.InstallNamespace,
				WithDeploymentSpec(depSpec.Spec),
				WithLabels(depSpec.Label),
			),
		)
	}
	return objs, nil
}

func BundlePermissionsGenerator(rv1 *convert.RegistryV1, opts Options) ([]client.Object, error) {
	permissions := rv1.CSV.Spec.InstallStrategy.StrategySpec.Permissions

	// If we're in AllNamespaces mode permissions will be treated as clusterPermissions
	if len(opts.TargetNamespaces) == 1 && opts.TargetNamespaces[0] == "" {
		return nil, nil
	}

	var objs []client.Object
	for _, ns := range opts.TargetNamespaces {
		for _, permission := range permissions {
			saName := saNameOrDefault(permission.ServiceAccountName)
			name, err := opts.UniqueNameGenerator(fmt.Sprintf("%s-%s", rv1.CSV.Name, saName), permission)
			if err != nil {
				return nil, err
			}

			objs = append(objs,
				GenerateRoleResource(name, ns, WithRules(permission.Rules...)),
				GenerateRoleBindingResource(
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

func BundleClusterPermissionsGenerator(rv1 *convert.RegistryV1, opts Options) ([]client.Object, error) {
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

	//nolint:prealloc
	var objs []client.Object
	for _, permission := range clusterPermissions {
		saName := saNameOrDefault(permission.ServiceAccountName)
		name, err := opts.UniqueNameGenerator(fmt.Sprintf("%s-%s", rv1.CSV.Name, saName), permission)
		if err != nil {
			return nil, err
		}
		objs = append(objs,
			GenerateClusterRoleResource(name, WithRules(permission.Rules...)),
			GenerateClusterRoleBindingResource(
				name,
				WithSubjects(rbacv1.Subject{Kind: "ServiceAccount", Namespace: opts.InstallNamespace, Name: saName}),
				WithRoleRef(rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: name}),
			),
		)
	}
	return objs, nil
}

func BundleServiceAccountGenerator(rv1 *convert.RegistryV1, opts Options) ([]client.Object, error) {
	allPermissions := append(
		rv1.CSV.Spec.InstallStrategy.StrategySpec.Permissions,
		rv1.CSV.Spec.InstallStrategy.StrategySpec.ClusterPermissions...,
	)

	var objs []client.Object
	serviceAccountNames := sets.Set[string]{}
	for _, permission := range allPermissions {
		serviceAccountNames.Insert(saNameOrDefault(permission.ServiceAccountName))
	}

	for _, serviceAccountName := range serviceAccountNames.UnsortedList() {
		// no need to generate the default service account
		if serviceAccountName != "default" {
			objs = append(objs, GenerateServiceAccountResource(serviceAccountName, opts.InstallNamespace))
		}
	}
	return objs, nil
}

func BundleCRDGenerator(rv1 *convert.RegistryV1, _ Options) ([]client.Object, error) {
	//nolint:prealloc
	var objs []client.Object
	for _, crd := range rv1.CRDs {
		objs = append(objs, crd.DeepCopy())
	}
	return objs, nil
}

func BundleResourceGenerator(rv1 *convert.RegistryV1, _ Options) ([]client.Object, error) {
	//nolint:prealloc
	var objs []client.Object
	for _, res := range rv1.Others {
		supported, namespaced := registrybundle.IsSupported(res.GetKind())
		if !supported {
			return nil, fmt.Errorf("bundle contains unsupported resource: Name: %v, Kind: %v", res.GetName(), res.GetKind())
		}

		obj := res.DeepCopy()
		if namespaced {
			obj.SetNamespace(res.GetNamespace())
		}

		objs = append(objs, obj)
	}
	return objs, nil
}

func saNameOrDefault(saName string) string {
	if saName == "" {
		return "default"
	}
	return saName
}
