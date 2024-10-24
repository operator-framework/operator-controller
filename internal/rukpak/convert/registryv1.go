package convert

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/go-openapi/spec"
	"helm.sh/helm/v3/pkg/chart"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/operator-registry/alpha/property"

	registry "github.com/operator-framework/operator-controller/internal/rukpak/operator-registry"
	"github.com/operator-framework/operator-controller/internal/rukpak/util"
)

type RegistryV1 struct {
	PackageName string
	CSV         v1alpha1.ClusterServiceVersion
	CRDs        []apiextensionsv1.CustomResourceDefinition
	Others      []unstructured.Unstructured
}

func LoadRegistryV1(ctx context.Context, rv1 fs.FS) (*RegistryV1, error) {
	l := log.FromContext(ctx)

	reg := RegistryV1{}
	annotationsFileData, err := fs.ReadFile(rv1, filepath.Join("metadata", "annotations.yaml"))
	if err != nil {
		return nil, err
	}
	annotationsFile := registry.AnnotationsFile{}
	if err := yaml.Unmarshal(annotationsFileData, &annotationsFile); err != nil {
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
			case "CustomResourceDefinition":
				crd := apiextensionsv1.CustomResourceDefinition{}
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(info.Object.(*unstructured.Unstructured).Object, &crd); err != nil {
					return err
				}
				reg.CRDs = append(reg.CRDs, crd)
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

	if err := copyMetadataPropertiesToCSV(&reg.CSV, rv1); err != nil {
		return nil, err
	}

	return &reg, nil
}

// copyMetadataPropertiesToCSV copies properties from `metadata/propeties.yaml` (in the filesystem fsys) into
// the CSV's `.metadata.annotations['olm.properties']` value, preserving any properties that are already
// present in the annotations.
func copyMetadataPropertiesToCSV(csv *v1alpha1.ClusterServiceVersion, fsys fs.FS) error {
	var allProperties []property.Property

	// First load existing properties from the CSV. We want to preserve these.
	if csvPropertiesJSON, ok := csv.Annotations["olm.properties"]; ok {
		var csvProperties []property.Property
		if err := json.Unmarshal([]byte(csvPropertiesJSON), &csvProperties); err != nil {
			return fmt.Errorf("failed to unmarshal csv.metadata.annotations['olm.properties']: %w", err)
		}
		allProperties = append(allProperties, csvProperties...)
	}

	// Next, load properties from the metadata/properties.yaml file, if it exists.
	metadataPropertiesJSON, err := fs.ReadFile(fsys, filepath.Join("metadata", "properties.yaml"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to read properties.yaml file: %w", err)
	}

	// If there are no properties, we can stick with whatever
	// was already present in the CSV annotations.
	if len(metadataPropertiesJSON) == 0 {
		return nil
	}

	// Otherwise, we need to parse the properties.yaml file and
	// append its properties into the CSV annotation.
	type registryV1Properties struct {
		Properties []property.Property `json:"properties"`
	}

	var metadataProperties registryV1Properties
	if err := yaml.Unmarshal(metadataPropertiesJSON, &metadataProperties); err != nil {
		return fmt.Errorf("failed to unmarshal metadata/properties.yaml: %w", err)
	}
	allProperties = append(allProperties, metadataProperties.Properties...)

	// Lastly re-marshal all the properties back into a JSON array and update the CSV annotation
	allPropertiesJSON, err := json.Marshal(allProperties)
	if err != nil {
		return fmt.Errorf("failed to marshal registry+v1 properties to json: %w", err)
	}
	csv.Annotations["olm.properties"] = string(allPropertiesJSON)
	return nil
}

type ToHelmChartOption func(*toChartOptions)

type toChartOptions struct {
	certProvider CertificateProvider
}

func (rv1 RegistryV1) ToHelmChart(options ...ToHelmChartOption) (*chart.Chart, error) {
	opts := &toChartOptions{}
	for _, o := range options {
		o(opts)
	}

	if len(rv1.CSV.Spec.APIServiceDefinitions.Owned) > 0 {
		return nil, fmt.Errorf("apiServiceDefintions are not supported")
	}

	if opts.certProvider == nil {
		if len(rv1.CSV.Spec.WebhookDefinitions) > 0 {
			return nil, fmt.Errorf("webhook definitions are not supported when a certificate provider is not configured")
		}
	}

	supportedInstallModes := getSupportedInstallModes(rv1.CSV.Spec.InstallModes)
	if supportedInstallModes&allNamespaces == 0 {
		return nil, fmt.Errorf("AllNamespaces install mode must be supported")
	}

	// If the CSV includes webhook definitions, should we fail if
	// it supports anything other than AllNamespaces install mode? In
	// OLMv0, it fails in this way, but only for bundles with conversion
	// webhooks.
	//
	// OLMv1 considers APIs to be cluster-scoped, and the upstream
	// Kubernetes api-server maintainers warned us explicitly not
	// to use namespaceSelectors in validating/mutating webhooks because
	// they were not designed to be used that way. They were designed so
	// core control plane components could be exempted during cluster
	// upgrade or bootstrap phases to ensure cluster stability.
	//
	// The alternative is to support all install modes, never set
	// namespaceSelectors in the webhook definitions, and use a
	// constant webhook metadata.name such that it is impossible to
	// install the same webhook multiple times targeting different
	// namespaces.
	//
	// For now, we'll go with the latter approach in order to have
	// _some_ support for as many CSVs as possible. We will also keep
	// OLMv0's behavior for CSVs with conversion webhooks (i.e. only
	// AllNamespaces install mode is allowed to be supported).
	for _, webhook := range rv1.CSV.Spec.WebhookDefinitions {
		if webhook.Type == v1alpha1.ConversionWebhook {
			if supportedInstallModes != allNamespaces {
				return nil, fmt.Errorf("invalid CSV: CSVs with conversion webhooks must support only AllNamespaces install mode")
			}
			if len(webhook.ConversionCRDs) == 0 {
				return nil, fmt.Errorf("invalid CSV: conversion webhooks definitions must specify at least one conversion CRD")
			}
		}
	}

	chrt := &chart.Chart{
		Metadata: &chart.Metadata{
			APIVersion:  "v2",
			Name:        rv1.PackageName,
			Version:     rv1.CSV.Spec.Version.String(),
			Description: rv1.CSV.Spec.Description,
			Keywords:    rv1.CSV.Spec.Keywords,
			Maintainers: convertMaintainers(rv1.CSV.Spec.Maintainers),
			Annotations: rv1.CSV.Annotations,
			Sources:     convertSpecLinks(rv1.CSV.Spec.Links),
			Home:        rv1.CSV.Spec.Provider.URL,
		},
	}
	if rv1.CSV.Spec.MinKubeVersion != "" {
		chrt.Metadata.KubeVersion = fmt.Sprintf(">= %s", rv1.CSV.Spec.MinKubeVersion)
	}

	/////////////////
	// Setup schema
	/////////////////
	valuesSchema, err := newValuesSchemaFile()
	if err != nil {
		return nil, err
	}
	chrt.Schema = valuesSchema

	templateFiles := make([]objectFile, 0,
		len(rv1.CRDs)+
			len(rv1.CSV.Spec.InstallStrategy.StrategySpec.Permissions)+
			len(rv1.CSV.Spec.InstallStrategy.StrategySpec.ClusterPermissions)+
			len(rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs)+
			len(rv1.CSV.Spec.WebhookDefinitions)+
			len(rv1.Others),
	)

	/////////////////
	// Setup RBAC
	/////////////////
	saFiles, err := newServiceAccountFiles(rv1.CSV)
	if err != nil {
		return nil, err
	}
	templateFiles = append(templateFiles, saFiles...)

	permissionFiles, err := newPermissionsFiles(rv1.CSV)
	if err != nil {
		return nil, err
	}
	templateFiles = append(templateFiles, permissionFiles...)

	clusterPermissionFiles, err := newClusterPermissionsFiles(rv1.CSV)
	if err != nil {
		return nil, err
	}
	templateFiles = append(templateFiles, clusterPermissionFiles...)

	deploymentConversions, err := rv1.deploymentConversionsFromCSV(opts.certProvider)
	if err != nil {
		return nil, err
	}

	for _, dc := range deploymentConversions {
		dep, err := dc.GenerateDeployment()
		if err != nil {
			return nil, fmt.Errorf("generate deployment: %v", err)
		}
		depFile, err := newFileForObject(dep)
		if err != nil {
			return nil, fmt.Errorf("generate parameterized file for deployment: %v", err)
		}
		templateFiles = append(templateFiles, *depFile)

		svc, err := dc.GenerateService()
		if err != nil {
			return nil, fmt.Errorf("generate service: %v", err)
		}
		if svc != nil {
			svcFile, err := newFileForObject(svc)
			if err != nil {
				return nil, fmt.Errorf("generate parameterized file for service: %v", err)
			}
			templateFiles = append(templateFiles, *svcFile)
		}

		///////////////////
		// Setup webhooks
		///////////////////
		validatingWebhooks, err := dc.GenerateValidatingWebhookConfigurations()
		if err != nil {
			return nil, fmt.Errorf("generate validating webhook configurations: %v", err)
		}
		for _, w := range validatingWebhooks {
			wFile, err := newFileForObject(&w)
			if err != nil {
				return nil, fmt.Errorf("generate parameterized file for validating webhook: %v", err)
			}
			templateFiles = append(templateFiles, *wFile)
		}

		mutatingWebhooks, err := dc.GenerateMutatingWebhookConfigurations()
		if err != nil {
			return nil, fmt.Errorf("generate mutating webhook configurations: %v", err)
		}
		for _, w := range mutatingWebhooks {
			wFile, err := newFileForObject(&w)
			if err != nil {
				return nil, fmt.Errorf("generate parameterized file for mutating webhook: %v", err)
			}
			templateFiles = append(templateFiles, *wFile)
		}

		if err := dc.ModifyConversionCRDs(); err != nil {
			return nil, fmt.Errorf("modify conversion CRDs: %v", err)
		}

		extraObjs, err := dc.AdditionalObjects()
		if err != nil {
			return nil, fmt.Errorf("generate additional objects: %v", err)
		}
		for _, obj := range extraObjs {
			f, err := newFileForObject(&obj)
			if err != nil {
				return nil, err
			}
			templateFiles = append(templateFiles, *f)
		}
	}

	//////////////////////////////////////
	// Add CRDs
	//////////////////////////////////////
	crdFiles, err := newCRDFiles(rv1.CRDs)
	if err != nil {
		return nil, err
	}
	templateFiles = append(templateFiles, crdFiles...)

	//////////////////////////////////////
	// Add all other static bundle objects
	//////////////////////////////////////
	for _, obj := range rv1.Others {
		f, err := newFileForObject(&obj)
		if err != nil {
			return nil, err
		}
		templateFiles = append(templateFiles, *f)
	}

	chrt.Templates = make([]*chart.File, 0, len(templateFiles))
	for _, pf := range templateFiles {
		chartTemplateFile, err := pf.ChartTemplateFile()
		if err != nil {
			return nil, err
		}
		chrt.Templates = append(chrt.Templates, chartTemplateFile)
	}
	return chrt, nil
}

func convertMaintainers(maintainers []v1alpha1.Maintainer) []*chart.Maintainer {
	chrtMaintainers := make([]*chart.Maintainer, 0, len(maintainers))
	for _, maintainer := range maintainers {
		chrtMaintainers = append(chrtMaintainers, &chart.Maintainer{
			Name:  maintainer.Name,
			Email: maintainer.Email,
		})
	}
	return chrtMaintainers
}

func convertSpecLinks(links []v1alpha1.AppLink) []string {
	chrtLinks := make([]string, 0, len(links))
	for _, link := range links {
		chrtLinks = append(chrtLinks, link.URL)
	}
	return chrtLinks
}

type objectFile struct {
	filename string
	obj      unstructured.Unstructured
}

func toUnstructured(obj client.Object) (*unstructured.Unstructured, error) {
	gvk := obj.GetObjectKind().GroupVersionKind()

	var u unstructured.Unstructured
	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("convert %s %q to unstructured: %w", gvk.Kind, obj.GetName(), err)
	}
	unstructured.RemoveNestedField(uObj, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(uObj, "status")
	u.Object = uObj
	u.SetGroupVersionKind(gvk)
	return &u, nil
}

func newFileForObject(obj client.Object) (*objectFile, error) {
	obj.SetNamespace("")
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, err
	}

	return &objectFile{
		filename: fileNameForObject(u.GroupVersionKind().GroupKind(), obj.GetName()),
		obj:      *u,
	}, nil
}

func (pf *objectFile) ChartTemplateFile() (*chart.File, error) {
	gvk := pf.obj.GetObjectKind().GroupVersionKind()
	yamlData, err := yaml.Marshal(&pf.obj)
	if err != nil {
		return nil, fmt.Errorf("parametrize %s %q: %w", gvk.Kind, pf.obj.GetName(), err)
	}

	return &chart.File{
		Name: filepath.Join("templates", pf.filename),
		Data: yamlData,
	}, nil
}

func newServiceAccountFiles(csv v1alpha1.ClusterServiceVersion) ([]objectFile, error) {
	saNames := sets.New[string]()

	for _, perms := range csv.Spec.InstallStrategy.StrategySpec.Permissions {
		saNames.Insert(perms.ServiceAccountName)
	}
	for _, perms := range csv.Spec.InstallStrategy.StrategySpec.ClusterPermissions {
		saNames.Insert(perms.ServiceAccountName)
	}
	var errs []error
	files := make([]objectFile, 0, len(saNames))
	for _, saName := range sets.List(saNames) {
		sa := corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: saName,
			},
		}
		sa.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ServiceAccount"))

		file, err := newFileForObject(&sa)
		if err != nil {
			errs = append(errs, fmt.Errorf("create template file for service account %q: %w", saName, err))
			continue
		}
		files = append(files, *file)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return files, nil
}

// newPermissionsFiles returns a list of template files for the necessary RBAC permissions from the CSV's permissions.
// A few implementation notes about how the helm templating should be done:
//   - The serviceAccountName and the rules are provided by the CSV.
//   - For now, we only support AllNamespaces install modes, which promotes all permissions to cluster permissions.
//     Therefore, newPermissionsFiles currently always creates ClusterRoles and ClusterRoleBindings
func newPermissionsFiles(csv v1alpha1.ClusterServiceVersion) ([]objectFile, error) {
	var (
		files = make([]objectFile, 0, len(csv.Spec.InstallStrategy.StrategySpec.Permissions))
		errs  []error
	)
	for _, perms := range csv.Spec.InstallStrategy.StrategySpec.Permissions {
		name := generateName(csv.Name, perms)
		clusterRole := rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Rules: perms.Rules,
		}
		clusterRole.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind("ClusterRole"))
		clusterRoleFile, err := newFileForObject(&clusterRole)
		if err != nil {
			errs = append(errs, fmt.Errorf("create template file for cluster role %q: %w", name, err))
			continue
		}
		files = append(files, *clusterRoleFile)

		clusterRoleBinding := rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      perms.ServiceAccountName,
					Namespace: "{{ .Release.Namespace }}",
				},
			},
		}
		clusterRoleBinding.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind("ClusterRoleBinding"))
		clusterRoleBindingFile, err := newFileForObject(&clusterRoleBinding)
		if err != nil {
			errs = append(errs, fmt.Errorf("create template file for cluster role binding %q: %w", name, err))
			continue
		}
		files = append(files, *clusterRoleBindingFile)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return files, nil
}

// newClusterPermissionsFiles returns a list of template files for the necessary RBAC permissions from the CSV's clusterPermissions.
// A few implementation notes about how the helm templating should be done:
//   - The serviceAccountName and the rules are provided by the CSV.
//   - clusterPermissions from the CSV are always translated to ClusterRole and ClusterRoleBindings.

func newClusterPermissionsFiles(csv v1alpha1.ClusterServiceVersion) ([]objectFile, error) {
	var (
		files = make([]objectFile, 0, len(csv.Spec.InstallStrategy.StrategySpec.ClusterPermissions))
		errs  []error
	)
	for _, perms := range csv.Spec.InstallStrategy.StrategySpec.ClusterPermissions {
		name := generateName(csv.Name, perms)
		clusterRole := rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Rules: perms.Rules,
		}
		clusterRole.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind("ClusterRole"))
		clusterRoleFile, err := newFileForObject(&clusterRole)
		if err != nil {
			errs = append(errs, fmt.Errorf("create template file for cluster role %q: %w", name, err))
			continue
		}
		files = append(files, *clusterRoleFile)

		clusterRoleBinding := rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     name,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      perms.ServiceAccountName,
					Namespace: "{{ .Release.Namespace }}",
				},
			},
		}
		clusterRoleBinding.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind("ClusterRoleBinding"))
		clusterRoleBindingFile, err := newFileForObject(&clusterRoleBinding)
		if err != nil {
			errs = append(errs, fmt.Errorf("create template file for cluster role binding %q: %w", name, err))
			continue
		}
		files = append(files, *clusterRoleBindingFile)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return files, nil
}

func newCRDFiles(crds []apiextensionsv1.CustomResourceDefinition) ([]objectFile, error) {
	var (
		files = make([]objectFile, 0, len(crds))
		errs  []error
	)

	for _, crd := range crds {
		crdFile, err := newFileForObject(&crd)
		if err != nil {
			errs = append(errs, fmt.Errorf("create template file for crd %q: %w", crd.GetName(), err))
			continue
		}
		files = append(files, *crdFile)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return files, nil
}

func (rv1 RegistryV1) findCRD(crdName string) (*apiextensionsv1.CustomResourceDefinition, error) {
	foundOwnedCRD := false
	for _, ownedCRD := range rv1.CSV.Spec.CustomResourceDefinitions.Owned {
		if ownedCRD.Name == crdName {
			foundOwnedCRD = true
			break
		}
	}
	var errs []error
	if !foundOwnedCRD {
		errs = append(errs, fmt.Errorf("CRD %q is not owned by the CSV", crdName))
	}

	var foundCRD *apiextensionsv1.CustomResourceDefinition
	for i := range rv1.CRDs {
		crd := &rv1.CRDs[i]
		if crd.Name == crdName {
			foundCRD = crd
			break
		}
	}
	if foundCRD == nil {
		errs = append(errs, fmt.Errorf("CRD %q is not found in the bundle", crdName))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return foundCRD, nil
}

func fileNameForObject(gk schema.GroupKind, name string) string {
	if gk.Group == "" {
		gk.Group = "core"
	}
	return fmt.Sprintf("%s.%s-%s.yaml", gk.Group, gk.Kind, name)
}

const maxNameLength = 63

func generateName(base string, o interface{}) string {
	hashStr, err := util.DeepHashObject(o)
	if err != nil {
		panic(err)
	}
	if len(base)+len(hashStr) > maxNameLength {
		base = base[:maxNameLength-len(hashStr)-1]
	}
	if base == "" {
		return hashStr
	}
	return fmt.Sprintf("%s-%s", base, hashStr)
}

func newValuesSchemaFile() ([]byte, error) {
	sch := getSchema()

	var jsonDataBuf bytes.Buffer
	enc := json.NewEncoder(&jsonDataBuf)
	enc.SetIndent("", "    ")
	if err := enc.Encode(sch); err != nil {
		return nil, err
	}
	return jsonDataBuf.Bytes(), nil
}

const (
	allNamespaces   = 1 << iota // 1 (0001)
	ownNamespace                // 2 (0010)
	singleNamespace             // 4 (0100)
	multiNamespace              // 8 (1000)
)

func getSupportedInstallModes(installModes []v1alpha1.InstallMode) int {
	supportedInstallModes := 0
	for _, installMode := range installModes {
		if installMode.Supported {
			switch installMode.Type {
			case v1alpha1.InstallModeTypeAllNamespaces:
				supportedInstallModes |= allNamespaces
			case v1alpha1.InstallModeTypeOwnNamespace:
				supportedInstallModes |= ownNamespace
			case v1alpha1.InstallModeTypeSingleNamespace:
				supportedInstallModes |= singleNamespace
			case v1alpha1.InstallModeTypeMultiNamespace:
				supportedInstallModes |= multiNamespace
			}
		}
	}
	return supportedInstallModes
}

// mergeMaps takes any number of maps and merges them into a new map.
// Later maps override keys from earlier maps if there are duplicates.
func mergeMaps[K comparable, V any](maps ...map[K]V) map[K]V {
	result := make(map[K]V)

	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}

	return result
}

func getSchema() *spec.Schema {
	return &spec.Schema{
		SchemaProps: spec.SchemaProps{
			Schema:               "http://json-schema.org/schema#",
			Type:                 spec.StringOrArray{"object"},
			Properties:           spec.SchemaProperties{},
			AdditionalProperties: &spec.SchemaOrBool{Allows: false},
		},
	}
}

func (rv1 RegistryV1) deploymentConversionsFromCSV(certProvider CertificateProvider) ([]deploymentConversion, error) {
	webhooksByDeploymentName := map[string][]v1alpha1.WebhookDescription{}
	for _, wh := range rv1.CSV.Spec.WebhookDefinitions {
		webhooksByDeploymentName[wh.DeploymentName] = append(webhooksByDeploymentName[wh.DeploymentName], wh)
	}

	deploymentConversions := make([]deploymentConversion, 0, len(webhooksByDeploymentName))
	for _, depSpec := range rv1.CSV.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		dc := deploymentConversion{
			csvName:                rv1.CSV.Name,
			csvMetadataAnnotations: rv1.CSV.Annotations,
			deploymentSpec:         depSpec,
			certProvider:           certProvider,
		}

		for _, wh := range webhooksByDeploymentName[depSpec.Name] {
			switch wh.Type {
			case v1alpha1.ValidatingAdmissionWebhook:
				dc.validatingWebhooks = append(dc.validatingWebhooks, wh)
			case v1alpha1.MutatingAdmissionWebhook:
				dc.mutatingWebhooks = append(dc.mutatingWebhooks, wh)
			case v1alpha1.ConversionWebhook:
				conversionCRDWebhook := conversionWebhook{
					webhookDescription: wh,
				}
				for _, conversionCRDName := range wh.ConversionCRDs {
					crd, err := rv1.findCRD(conversionCRDName)
					if err != nil {
						return nil, err
					}
					conversionCRDWebhook.crds = append(conversionCRDWebhook.crds, crd)
				}
				dc.conversionWebhooks = append(dc.conversionWebhooks, conversionCRDWebhook)
			}
		}
		delete(webhooksByDeploymentName, depSpec.Name)
		deploymentConversions = append(deploymentConversions, dc)
	}

	var errs []error
	for deploymentName, webhookDescriptions := range webhooksByDeploymentName {
		for _, webhookDescription := range webhookDescriptions {
			switch webhookDescription.Type {
			case v1alpha1.ValidatingAdmissionWebhook, v1alpha1.MutatingAdmissionWebhook:
				errs = append(errs, fmt.Errorf("could not find deployment %q for admission webhook %q", deploymentName, webhookDescription.GenerateName))
			case v1alpha1.ConversionWebhook:
				for _, conversionCRD := range webhookDescription.ConversionCRDs {
					errs = append(errs, fmt.Errorf("could not find deployment %q for conversion webhook for CRD %q", deploymentName, conversionCRD))
				}
			}
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return deploymentConversions, nil
}

type deploymentConversion struct {
	csvName                string
	csvMetadataAnnotations map[string]string
	deploymentSpec         v1alpha1.StrategyDeploymentSpec
	validatingWebhooks     []v1alpha1.WebhookDescription
	mutatingWebhooks       []v1alpha1.WebhookDescription
	conversionWebhooks     []conversionWebhook
	certProvider           CertificateProvider
}

type conversionWebhook struct {
	webhookDescription v1alpha1.WebhookDescription
	crds               []*apiextensionsv1.CustomResourceDefinition
}

func (c deploymentConversion) GenerateDeployment() (*appsv1.Deployment, error) {
	if c.deploymentSpec.Name == "" {
		return nil, errors.New("deployment spec has no name, but name is required")
	}

	dep := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:   c.deploymentSpec.Name,
			Labels: c.deploymentSpec.Label,
		},
		Spec: c.deploymentSpec.Spec,
	}
	dep.SetGroupVersionKind(appsv1.SchemeGroupVersion.WithKind("Deployment"))
	dep.Spec.Template.SetAnnotations(mergeMaps(dep.Spec.Template.Annotations, c.csvMetadataAnnotations))
	dep.Spec.Template.Annotations["olm.targetNamespaces"] = ""

	// Hardcode the deployment with RevisionHistoryLimit=1 (something OLMv0 does, not sure why)
	dep.Spec.RevisionHistoryLimit = ptr.To(int32(1))

	if c.certProvider != nil {
		secretInfo := c.certProvider.CertSecretInfo(c)
		// Need to inject volumes and volume mounts for the cert secret
		dep.Spec.Template.Spec.Volumes = slices.DeleteFunc(dep.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool {
			return v.Name == "apiservice-cert" || v.Name == "webhook-cert"
		})
		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes,
			corev1.Volume{
				Name: "apiservice-cert",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: secretInfo.SecretName,
						Items: []corev1.KeyToPath{
							{
								Key:  secretInfo.CertificateKey,
								Path: "apiserver.crt",
							},
							{
								Key:  secretInfo.PrivateKeyKey,
								Path: "apiserver.key",
							},
						},
					},
				},
			},
			corev1.Volume{
				Name: "webhook-cert",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: secretInfo.SecretName,
						Items: []corev1.KeyToPath{
							{
								Key:  secretInfo.CertificateKey,
								Path: "tls.crt",
							},
							{
								Key:  secretInfo.PrivateKeyKey,
								Path: "tls.key",
							},
						},
					},
				},
			},
		)

		volumeMounts := []corev1.VolumeMount{
			{Name: "apiservice-cert", MountPath: "/apiserver.local.config/certificates"},
			{Name: "webhook-cert", MountPath: "/tmp/k8s-webhook-server/serving-certs"},
		}
		for i := range dep.Spec.Template.Spec.Containers {
			dep.Spec.Template.Spec.Containers[i].VolumeMounts = slices.DeleteFunc(dep.Spec.Template.Spec.Containers[i].VolumeMounts, func(vm corev1.VolumeMount) bool {
				return vm.Name == "apiservice-cert" || vm.Name == "webhook-cert"
			})
			dep.Spec.Template.Spec.Containers[i].VolumeMounts = append(dep.Spec.Template.Spec.Containers[i].VolumeMounts, volumeMounts...)
		}
	}
	return &dep, nil
}

func (c deploymentConversion) CSVName() string {
	return c.csvName
}

func (c deploymentConversion) DeploymentName() string {
	return c.deploymentSpec.Name
}

func (c deploymentConversion) ServiceName() string {
	return fmt.Sprintf("%s-service", strings.ReplaceAll(c.deploymentSpec.Name, ".", "-"))
}

func (c deploymentConversion) GenerateService() (*corev1.Service, error) {
	allWebhooks := make([]v1alpha1.WebhookDescription, 0, len(c.validatingWebhooks)+len(c.mutatingWebhooks)+len(c.conversionWebhooks))
	allWebhooks = append(allWebhooks, c.validatingWebhooks...)
	allWebhooks = append(allWebhooks, c.mutatingWebhooks...)
	for _, cw := range c.conversionWebhooks {
		allWebhooks = append(allWebhooks, cw.webhookDescription)
	}
	if len(allWebhooks) == 0 {
		return nil, nil
	}

	var (
		servicePorts = sets.Set[corev1.ServicePort]{}
	)
	for _, wh := range allWebhooks {
		containerPort := int32(443)
		if wh.ContainerPort > 0 {
			containerPort = wh.ContainerPort
		}

		targetPort := intstr.FromInt32(containerPort)
		if wh.TargetPort != nil {
			targetPort = *wh.TargetPort
		}
		sp := corev1.ServicePort{
			Name:       strconv.Itoa(int(containerPort)),
			Port:       containerPort,
			TargetPort: targetPort,
		}
		servicePorts.Insert(sp)
	}

	ports := servicePorts.UnsortedList()
	slices.SortFunc(ports, func(a, b corev1.ServicePort) int {
		return cmp.Compare(a.Name, b.Name)
	})
	service := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: c.ServiceName(),
		},
		Spec: corev1.ServiceSpec{
			Ports:    ports,
			Selector: c.deploymentSpec.Spec.Selector.MatchLabels,
		},
	}
	service.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))

	if c.certProvider != nil {
		if err := c.certProvider.ModifyService(&service, c); err != nil {
			return nil, fmt.Errorf("certificate provider failed to modify service: %v", err)
		}
	}

	return &service, nil
}

func (c deploymentConversion) GenerateValidatingWebhookConfigurations() ([]admissionregistrationv1.ValidatingWebhookConfiguration, error) {
	validatingWebhookConfigurations := []admissionregistrationv1.ValidatingWebhookConfiguration{}
	for _, wh := range c.validatingWebhooks {
		vwc := admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: wh.GenerateName,
			},
			Webhooks: []admissionregistrationv1.ValidatingWebhook{
				{
					Name:                    wh.GenerateName,
					Rules:                   wh.Rules,
					FailurePolicy:           wh.FailurePolicy,
					MatchPolicy:             wh.MatchPolicy,
					ObjectSelector:          wh.ObjectSelector,
					SideEffects:             wh.SideEffects,
					TimeoutSeconds:          wh.TimeoutSeconds,
					AdmissionReviewVersions: wh.AdmissionReviewVersions,
					ClientConfig: admissionregistrationv1.WebhookClientConfig{
						Service: &admissionregistrationv1.ServiceReference{
							Namespace: "{{ .Release.Namespace }}",
							Name:      c.ServiceName(),
							Path:      wh.WebhookPath,
							Port:      &wh.ContainerPort,
						},
					},
				},
			},
		}
		vwc.SetGroupVersionKind(admissionregistrationv1.SchemeGroupVersion.WithKind("ValidatingWebhookConfiguration"))

		if c.certProvider != nil {
			if err := c.certProvider.ModifyValidatingWebhookConfiguration(&vwc, c); err != nil {
				return nil, fmt.Errorf("certificate provider failed to modify validating webhook configuration: %v", err)
			}
		}
		validatingWebhookConfigurations = append(validatingWebhookConfigurations, vwc)
	}
	return validatingWebhookConfigurations, nil
}

func (c deploymentConversion) GenerateMutatingWebhookConfigurations() ([]admissionregistrationv1.MutatingWebhookConfiguration, error) {
	mutatingWebhookConfigurations := []admissionregistrationv1.MutatingWebhookConfiguration{}
	for _, wh := range c.mutatingWebhooks {
		mwc := admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: wh.GenerateName,
			},
			Webhooks: []admissionregistrationv1.MutatingWebhook{
				{
					Name:                    wh.GenerateName,
					Rules:                   wh.Rules,
					FailurePolicy:           wh.FailurePolicy,
					MatchPolicy:             wh.MatchPolicy,
					ObjectSelector:          wh.ObjectSelector,
					SideEffects:             wh.SideEffects,
					TimeoutSeconds:          wh.TimeoutSeconds,
					AdmissionReviewVersions: wh.AdmissionReviewVersions,
					ClientConfig: admissionregistrationv1.WebhookClientConfig{
						Service: &admissionregistrationv1.ServiceReference{
							Namespace: "{{ .Release.Namespace }}",
							Name:      c.ServiceName(),
							Path:      wh.WebhookPath,
							Port:      &wh.ContainerPort,
						},
					},
					ReinvocationPolicy: wh.ReinvocationPolicy,
				},
			},
		}
		mwc.SetGroupVersionKind(admissionregistrationv1.SchemeGroupVersion.WithKind("MutatingWebhookConfiguration"))

		if c.certProvider != nil {
			if err := c.certProvider.ModifyMutatingWebhookConfiguration(&mwc, c); err != nil {
				return nil, fmt.Errorf("certificate provider failed to modify mutating webhook configuration: %v", err)
			}
		}
		mutatingWebhookConfigurations = append(mutatingWebhookConfigurations, mwc)
	}
	return mutatingWebhookConfigurations, nil
}

func (c deploymentConversion) ModifyConversionCRDs() error {
	for _, cw := range c.conversionWebhooks {
		conversionWebhookPath := "/"
		if cw.webhookDescription.WebhookPath != nil {
			conversionWebhookPath = *cw.webhookDescription.WebhookPath
		}

		for _, crd := range cw.crds {
			if crd.Spec.PreserveUnknownFields {
				return fmt.Errorf("CRD %q sets spec.preserveUnknownFields=true; must be false to let API Server call webhook to do the conversion", crd.Name)
			}

			crd.Spec.Conversion = &apiextensionsv1.CustomResourceConversion{
				Strategy: apiextensionsv1.WebhookConverter,
				Webhook: &apiextensionsv1.WebhookConversion{
					ClientConfig: &apiextensionsv1.WebhookClientConfig{
						Service: &apiextensionsv1.ServiceReference{
							Namespace: "{{ .Release.Namespace }}",
							Name:      c.ServiceName(),
							Path:      &conversionWebhookPath,
							Port:      &cw.webhookDescription.ContainerPort,
						},
					},
					ConversionReviewVersions: cw.webhookDescription.AdmissionReviewVersions,
				},
			}

			if c.certProvider != nil {
				if err := c.certProvider.ModifyCustomResourceDefinition(crd, c); err != nil {
					return fmt.Errorf("certificate provider failed to modify custom resource definition: %v", err)
				}
			}
		}
	}
	return nil
}

func (c deploymentConversion) AdditionalObjects() ([]unstructured.Unstructured, error) {
	numWebhooks := len(c.validatingWebhooks) + len(c.mutatingWebhooks) + len(c.conversionWebhooks)
	if c.certProvider == nil || numWebhooks == 0 {
		return nil, nil
	}
	return c.certProvider.AdditionalObjects(c)
}
