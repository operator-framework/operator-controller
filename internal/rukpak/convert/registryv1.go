package convert

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"helm.sh/helm/v3/pkg/chart"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	registrybundle "github.com/operator-framework/operator-registry/pkg/lib/bundle"

	registry "github.com/operator-framework/operator-controller/internal/rukpak/operator-registry"
	"github.com/operator-framework/operator-controller/internal/rukpak/util"
)

type RegistryV1 struct {
	PackageName string
	CSV         v1alpha1.ClusterServiceVersion
	Others      []unstructured.Unstructured
}

type Plain struct {
	Objects []client.Object
}

func RegistryV1ToHelmChart(ctx context.Context, rv1 fs.FS, installNamespace string, watchNamespaces []string) (*chart.Chart, error) {
	l := log.FromContext(ctx)

	reg := RegistryV1{}
	fileData, err := fs.ReadFile(rv1, filepath.Join("metadata", "annotations.yaml"))
	if err != nil {
		return nil, err
	}
	annotationsFile := registry.AnnotationsFile{}
	if err := yaml.Unmarshal(fileData, &annotationsFile); err != nil {
		return nil, err
	}
	reg.PackageName = annotationsFile.Annotations.PackageName

	const manifestsDir = "manifests"
	if err := fs.WalkDir(rv1, manifestsDir, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if path == manifestsDir {
				return nil
			}
			return fmt.Errorf("subdirectories are not allowed within the %q directory of the bundle image filesystem: found %q", manifestsDir, path)
		}
		manifestFile, err := rv1.Open(path)
		if err != nil {
			return err
		}
		defer func() {
			if err := manifestFile.Close(); err != nil {
				l.Error(err, "error closing file", "path", path)
			}
		}()

		result := resource.NewLocalBuilder().Unstructured().Flatten().Stream(manifestFile, path).Do()
		if err := result.Err(); err != nil {
			return err
		}
		if err := result.Visit(func(info *resource.Info, err error) error {
			if err != nil {
				return err
			}
			switch info.Object.GetObjectKind().GroupVersionKind().Kind {
			case "ClusterServiceVersion":
				csv := v1alpha1.ClusterServiceVersion{}
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(info.Object.(*unstructured.Unstructured).Object, &csv); err != nil {
					return err
				}
				reg.CSV = csv
			default:
				reg.Others = append(reg.Others, *info.Object.(*unstructured.Unstructured))
			}
			return nil
		}); err != nil {
			return fmt.Errorf("error parsing objects in %q: %v", path, err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return toChart(reg, installNamespace, watchNamespaces)
}

func toChart(in RegistryV1, installNamespace string, watchNamespaces []string) (*chart.Chart, error) {
	plain, err := Convert(in, installNamespace, watchNamespaces)
	if err != nil {
		return nil, err
	}

	chrt := &chart.Chart{Metadata: &chart.Metadata{}}
	chrt.Metadata.Annotations = in.CSV.GetAnnotations()
	for _, obj := range plain.Objects {
		jsonData, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(jsonData)
		chrt.Templates = append(chrt.Templates, &chart.File{
			Name: fmt.Sprintf("object-%x.json", hash[0:8]),
			Data: jsonData,
		})
	}

	return chrt, nil
}

func validateTargetNamespaces(supportedInstallModes sets.Set[string], installNamespace string, targetNamespaces []string) error {
	set := sets.New[string](targetNamespaces...)
	switch {
	case set.Len() == 0 || (set.Len() == 1 && set.Has("")):
		if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeAllNamespaces)) {
			return nil
		}
		return fmt.Errorf("supported install modes %v do not support targeting all namespaces", sets.List(supportedInstallModes))
	case set.Len() == 1 && !set.Has(""):
		if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeSingleNamespace)) {
			return nil
		}
		if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeOwnNamespace)) && targetNamespaces[0] == installNamespace {
			return nil
		}
	default:
		if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeMultiNamespace)) && !set.Has("") {
			return nil
		}
	}
	return fmt.Errorf("supported install modes %v do not support target namespaces %v", sets.List[string](supportedInstallModes), targetNamespaces)
}

func saNameOrDefault(saName string) string {
	if saName == "" {
		return "default"
	}
	return saName
}

func Convert(in RegistryV1, installNamespace string, targetNamespaces []string) (*Plain, error) {
	if installNamespace == "" {
		installNamespace = in.CSV.Annotations["operatorframework.io/suggested-namespace"]
	}
	if installNamespace == "" {
		installNamespace = fmt.Sprintf("%s-system", in.PackageName)
	}
	supportedInstallModes := sets.New[string]()
	for _, im := range in.CSV.Spec.InstallModes {
		if im.Supported {
			supportedInstallModes.Insert(string(im.Type))
		}
	}
	if len(targetNamespaces) == 0 {
		if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeAllNamespaces)) {
			targetNamespaces = []string{""}
		} else if supportedInstallModes.Has(string(v1alpha1.InstallModeTypeOwnNamespace)) {
			targetNamespaces = []string{installNamespace}
		}
	}

	if err := validateTargetNamespaces(supportedInstallModes, installNamespace, targetNamespaces); err != nil {
		return nil, err
	}

	if len(in.CSV.Spec.APIServiceDefinitions.Owned) > 0 {
		return nil, fmt.Errorf("apiServiceDefintions are not supported")
	}

	if len(in.CSV.Spec.WebhookDefinitions) > 0 {
		return nil, fmt.Errorf("webhookDefinitions are not supported")
	}

	deployments := []appsv1.Deployment{}
	serviceAccounts := map[string]corev1.ServiceAccount{}
	for _, depSpec := range in.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		annotations := util.MergeMaps(in.CSV.Annotations, depSpec.Spec.Template.Annotations)
		annotations["olm.targetNamespaces"] = strings.Join(targetNamespaces, ",")
		depSpec.Spec.Template.Annotations = annotations
		deployments = append(deployments, appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Deployment",
				APIVersion: appsv1.SchemeGroupVersion.String(),
			},

			ObjectMeta: metav1.ObjectMeta{
				Namespace: installNamespace,
				Name:      depSpec.Name,
				Labels:    depSpec.Label,
			},
			Spec: depSpec.Spec,
		})
		saName := saNameOrDefault(depSpec.Spec.Template.Spec.ServiceAccountName)
		serviceAccounts[saName] = newServiceAccount(installNamespace, saName)
	}

	// NOTES:
	//   1. There's an extra Role for OperatorConditions: get/update/patch; resourceName=csv.name
	//        - This is managed by the OperatorConditions controller here: https://github.com/operator-framework/operator-lifecycle-manager/blob/9ced412f3e263b8827680dc0ad3477327cd9a508/pkg/controller/operators/operatorcondition_controller.go#L106-L109
	//   2. There's an extra RoleBinding for the above mentioned role.
	//        - Every SA mentioned in the OperatorCondition.spec.serviceAccounts is a subject for this role binding: https://github.com/operator-framework/operator-lifecycle-manager/blob/9ced412f3e263b8827680dc0ad3477327cd9a508/pkg/controller/operators/operatorcondition_controller.go#L171-L177
	//   3. strategySpec.permissions are _also_ given a clusterrole/clusterrole binding.
	//  		- (for AllNamespaces mode only?)
	//			- (where does the extra namespaces get/list/watch rule come from?)

	roles := []rbacv1.Role{}
	roleBindings := []rbacv1.RoleBinding{}
	clusterRoles := []rbacv1.ClusterRole{}
	clusterRoleBindings := []rbacv1.ClusterRoleBinding{}

	permissions := in.CSV.Spec.InstallStrategy.StrategySpec.Permissions
	clusterPermissions := in.CSV.Spec.InstallStrategy.StrategySpec.ClusterPermissions
	allPermissions := append(permissions, clusterPermissions...)

	// Create all the service accounts
	for _, permission := range allPermissions {
		saName := saNameOrDefault(permission.ServiceAccountName)
		if _, ok := serviceAccounts[saName]; !ok {
			serviceAccounts[saName] = newServiceAccount(installNamespace, saName)
		}
	}

	// If we're in AllNamespaces mode, promote the permissions to clusterPermissions
	if len(targetNamespaces) == 1 && targetNamespaces[0] == "" {
		for _, p := range permissions {
			p.Rules = append(p.Rules, rbacv1.PolicyRule{
				Verbs:     []string{"get", "list", "watch"},
				APIGroups: []string{corev1.GroupName},
				Resources: []string{"namespaces"},
			})
		}
		clusterPermissions = append(clusterPermissions, permissions...)
		permissions = nil
	}

	for _, ns := range targetNamespaces {
		for _, permission := range permissions {
			saName := saNameOrDefault(permission.ServiceAccountName)
			name, err := generateName(fmt.Sprintf("%s-%s", in.CSV.Name, saName), permission)
			if err != nil {
				return nil, err
			}
			roles = append(roles, newRole(ns, name, permission.Rules))
			roleBindings = append(roleBindings, newRoleBinding(ns, name, name, installNamespace, saName))
		}
	}

	for _, permission := range clusterPermissions {
		saName := saNameOrDefault(permission.ServiceAccountName)
		name, err := generateName(fmt.Sprintf("%s-%s", in.CSV.Name, saName), permission)
		if err != nil {
			return nil, err
		}
		clusterRoles = append(clusterRoles, newClusterRole(name, permission.Rules))
		clusterRoleBindings = append(clusterRoleBindings, newClusterRoleBinding(name, name, installNamespace, saName))
	}

	objs := []client.Object{}
	for _, obj := range serviceAccounts {
		obj := obj
		if obj.GetName() != "default" {
			objs = append(objs, &obj)
		}
	}
	for _, obj := range roles {
		obj := obj
		objs = append(objs, &obj)
	}
	for _, obj := range roleBindings {
		obj := obj
		objs = append(objs, &obj)
	}
	for _, obj := range clusterRoles {
		obj := obj
		objs = append(objs, &obj)
	}
	for _, obj := range clusterRoleBindings {
		obj := obj
		objs = append(objs, &obj)
	}
	for _, obj := range in.Others {
		obj := obj
		supported, namespaced := registrybundle.IsSupported(obj.GetKind())
		if !supported {
			return nil, fmt.Errorf("bundle contains unsupported resource: Name: %v, Kind: %v", obj.GetName(), obj.GetKind())
		}
		if namespaced {
			obj.SetNamespace(installNamespace)
		}
		objs = append(objs, &obj)
	}
	for _, obj := range deployments {
		obj := obj
		objs = append(objs, &obj)
	}
	return &Plain{Objects: objs}, nil
}

const maxNameLength = 63

func generateName(base string, o interface{}) (string, error) {
	hashStr, err := util.DeepHashObject(o)
	if err != nil {
		return "", err
	}
	if len(base)+len(hashStr) > maxNameLength {
		base = base[:maxNameLength-len(hashStr)-1]
	}

	return fmt.Sprintf("%s-%s", base, hashStr), nil
}

func newServiceAccount(namespace, name string) corev1.ServiceAccount {
	return corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
}

func newRole(namespace, name string, rules []rbacv1.PolicyRule) rbacv1.Role {
	return rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Role",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Rules: rules,
	}
}

func newClusterRole(name string, rules []rbacv1.PolicyRule) rbacv1.ClusterRole {
	return rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Rules: rules,
	}
}

func newRoleBinding(namespace, name, roleName, saNamespace string, saNames ...string) rbacv1.RoleBinding {
	subjects := make([]rbacv1.Subject, 0, len(saNames))
	for _, saName := range saNames {
		subjects = append(subjects, rbacv1.Subject{
			Kind:      "ServiceAccount",
			Namespace: saNamespace,
			Name:      saName,
		})
	}
	return rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Subjects: subjects,
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     roleName,
		},
	}
}

func newClusterRoleBinding(name, roleName, saNamespace string, saNames ...string) rbacv1.ClusterRoleBinding {
	subjects := make([]rbacv1.Subject, 0, len(saNames))
	for _, saName := range saNames {
		subjects = append(subjects, rbacv1.Subject{
			Kind:      "ServiceAccount",
			Namespace: saNamespace,
			Name:      saName,
		})
	}
	return rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Subjects: subjects,
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     roleName,
		},
	}
}
