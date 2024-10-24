package v2

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/go-openapi/spec"
	"golang.org/x/exp/maps"
	"helm.sh/helm/v3/pkg/chart"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/rukpak/convert"
	"github.com/operator-framework/operator-controller/internal/rukpak/convert/v2/internal/parametrize"
	"github.com/operator-framework/operator-controller/internal/rukpak/util"
)

func RegistryV1ToHelmChart(ctx context.Context, rv1FS fs.FS) (*chart.Chart, error) {
	rv1, err := convert.LoadRegistryV1(ctx, rv1FS)
	if err != nil {
		return nil, err
	}

	if len(rv1.CSV.Spec.APIServiceDefinitions.Owned) > 0 {
		return nil, fmt.Errorf("apiServiceDefintions are not supported")
	}

	if len(rv1.CSV.Spec.WebhookDefinitions) > 0 {
		return nil, fmt.Errorf("webhookDefinitions are not supported")
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

	watchNsConfig := getWatchNamespacesSchema(rv1.CSV.Spec.InstallModes)

	/////////////////
	// Setup schema
	/////////////////
	valuesSchema, err := newValuesSchemaFile(watchNsConfig)
	if err != nil {
		return nil, err
	}
	chrt.Schema = valuesSchema

	/////////////////
	// Setup helpers
	/////////////////
	chrt.Templates = append(chrt.Templates, newTargetNamespacesTemplateHelper(watchNsConfig))
	chrt.Templates = append(chrt.Templates, newDeploymentsTemplateHelper(rv1.CSV))

	/////////////////
	// Setup RBAC
	/////////////////
	saFiles, err := newServiceAccountFiles(rv1.CSV)
	if err != nil {
		return nil, err
	}
	chrt.Templates = append(chrt.Templates, saFiles...)

	permissionFiles, err := newPermissionsFiles(rv1.CSV)
	if err != nil {
		return nil, err
	}
	chrt.Templates = append(chrt.Templates, permissionFiles...)

	clusterPermissionFiles, err := newClusterPermissionsFiles(rv1.CSV)
	if err != nil {
		return nil, err
	}
	chrt.Templates = append(chrt.Templates, clusterPermissionFiles...)

	///////////////////
	// Setup Deployment
	///////////////////
	deploymentFiles, err := newDeploymentFiles(rv1.CSV)
	if err != nil {
		return nil, err
	}
	chrt.Templates = append(chrt.Templates, deploymentFiles...)

	//////////////////////////////////////
	// Add all other static bundle objects
	//////////////////////////////////////
	for _, obj := range rv1.Others {
		f, err := newFile(&obj)
		if err != nil {
			return nil, err
		}
		chrt.Templates = append(chrt.Templates, f)
	}

	return chrt, nil
}

func convertMaintainers(maintainers []v1alpha1.Maintainer) []*chart.Maintainer {
	var chrtMaintainers []*chart.Maintainer
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

func newFile(obj client.Object, instructions ...parametrize.Instruction) (*chart.File, error) {
	obj.SetNamespace("")
	gvk := obj.GetObjectKind().GroupVersionKind()

	// Execute the parametrize instructions on the object.
	var u unstructured.Unstructured
	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("convert %s %q to unstructured: %w", gvk.Kind, obj.GetName(), err)
	}
	u.Object = uObj
	u.SetGroupVersionKind(gvk)

	yamlData, err := parametrize.Execute(u, instructions...)
	if err != nil {
		return nil, fmt.Errorf("parametrize %s %q: %w", gvk.Kind, obj.GetName(), err)
	}

	return &chart.File{
		Name: fileNameForObject(gvk.GroupKind(), obj.GetName()),
		Data: yamlData,
	}, nil
}

func newServiceAccountFiles(csv v1alpha1.ClusterServiceVersion) ([]*chart.File, error) {
	saNames := sets.New[string]()

	for _, perms := range csv.Spec.InstallStrategy.StrategySpec.Permissions {
		saNames.Insert(perms.ServiceAccountName)
	}
	for _, perms := range csv.Spec.InstallStrategy.StrategySpec.ClusterPermissions {
		saNames.Insert(perms.ServiceAccountName)
	}
	var errs []error
	files := make([]*chart.File, 0, len(saNames))
	for _, saName := range sets.List(saNames) {
		sa := corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: saName,
			},
		}
		sa.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ServiceAccount"))

		file, err := newFile(&sa)
		if err != nil {
			errs = append(errs, fmt.Errorf("create template file for service account %q: %w", saName, err))
			continue
		}
		files = append(files, file)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return files, nil
}

// newPermissionsFiles returns a list of template files for the necessary RBAC permissions from the CSV's permissions.
// A few implementation notes about how the helm templating should be done:
//   - The serviceAccountName and the rules are provided by the CSV.
//   - if .Values.watchNamespaces corresponds to watching all namespaces, then ClusterRole and ClusterRoleBindings should be used
//   - if .Values.watchNamespaces corresponds to watching specific namespaces, then Role and RoleBindings should be used. In this case,
//     the Role and RoleBinding should be created in each namespace in .Values.watchNamespaces, and the RoleBinding subject to be the
//     ServiceAccount in the install namespace.
func newPermissionsFiles(csv v1alpha1.ClusterServiceVersion) ([]*chart.File, error) {
	var (
		files = make([]*chart.File, 0, len(csv.Spec.InstallStrategy.StrategySpec.Permissions))
		errs  []error
	)
	for _, perms := range csv.Spec.InstallStrategy.StrategySpec.Permissions {
		rulesMap := map[string][]rbacv1.PolicyRule{
			"rules": perms.Rules,
		}
		yamlRules, err := yaml.Marshal(rulesMap)
		if err != nil {
			errs = append(errs, fmt.Errorf("marshal rules for service account %q: %w", perms.ServiceAccountName, err))
			continue
		}
		name := generateName(csv.Name, perms)
		yamlData := []byte(fmt.Sprintf(`{{- $installNamespace := .Release.Namespace -}}
{{- $targetNamespaces := include "olm.targetNamespaces" . -}}
{{- $name := %[1]q -}}
{{- $serviceAccountName := %[2]q -}}

{{- $promoteToClusterRole := (eq $targetNamespaces "") -}}

{{- define "rules-%[1]s" -}}
%[3]s
{{ end -}}

{{- if $promoteToClusterRole -}}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ $name }}
{{ template "rules-%[1]s" }}
{{- else -}}
{{- range (split "," $targetNamespaces) -}}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: {{ $name }}
  namespace: {{ . }}
{{ template "rules-%[1]s" }}
{{- end -}}
{{- end -}}

{{- if $promoteToClusterRole -}}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ $name }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: {{ $name }}
subjects:
- kind: ServiceAccount
  name: {{ $serviceAccountName }}
  namespace: {{ $installNamespace }}
{{- else -}}
{{- range (split "," $targetNamespaces) -}}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: {{ $name }}
  namespace: {{ . }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: {{ $name }}
subjects:
- kind: ServiceAccount
  name: {{ $serviceAccountName }}
  namespace: {{ $installNamespace }}
{{ end -}}
{{- end -}}
`, name, perms.ServiceAccountName, yamlRules))
		files = append(files, &chart.File{
			Name: fmt.Sprintf("templates/rbac.authorization.k8s.io-csv.permissions-%s.yaml", perms.ServiceAccountName),
			Data: yamlData,
		})
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

func newClusterPermissionsFiles(csv v1alpha1.ClusterServiceVersion) ([]*chart.File, error) {
	var (
		files = make([]*chart.File, 0, len(csv.Spec.InstallStrategy.StrategySpec.ClusterPermissions))
		errs  []error
	)
	for _, perms := range csv.Spec.InstallStrategy.StrategySpec.ClusterPermissions {
		rulesMap := map[string][]rbacv1.PolicyRule{
			"rules": perms.Rules,
		}
		yamlRules, err := yaml.Marshal(rulesMap)
		if err != nil {
			errs = append(errs, fmt.Errorf("marshal rules for service account %q: %w", perms.ServiceAccountName, err))
			continue
		}

		name := generateName(csv.Name, perms)
		yamlData := []byte(fmt.Sprintf(`{{- $name := %q -}}
{{- $serviceAccountName := %q -}}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ $name }}
%s
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ $name }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: {{ $name }}
subjects:
- kind: ServiceAccount
  name: {{ $serviceAccountName }}
  namespace: {{ .Release.Namespace }}
`, name, perms.ServiceAccountName, yamlRules))
		files = append(files, &chart.File{
			Name: fmt.Sprintf("templates/rbac.authorization.k8s.io-csv.clusterPermissions-%s.yaml", perms.ServiceAccountName),
			Data: yamlData,
		})
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return files, nil
}

func newDeploymentsTemplateHelper(csv v1alpha1.ClusterServiceVersion) *chart.File {
	var sb bytes.Buffer
	for _, depSpec := range csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		snippets := map[string]string{}

		affinityJSON, _ := json.Marshal(depSpec.Spec.Template.Spec.Affinity)
		snippets["affinity"] = string(affinityJSON)

		nodeSelectorJSON, _ := json.Marshal(depSpec.Spec.Template.Spec.NodeSelector)
		snippets["nodeSelector"] = string(nodeSelectorJSON)

		selectorJSON, _ := json.Marshal(depSpec.Spec.Selector)
		snippets["selector"] = string(selectorJSON)

		tolerationsJSON, _ := json.Marshal(depSpec.Spec.Template.Spec.Tolerations)
		snippets["tolerations"] = string(tolerationsJSON)

		volumesJSON, _ := json.Marshal(depSpec.Spec.Template.Spec.Volumes)
		snippets["volumes"] = string(volumesJSON)

		for _, container := range depSpec.Spec.Template.Spec.Containers {
			containerJSON, _ := json.Marshal(container)
			snippets[container.Name] = string(containerJSON)
		}

		for _, fieldName := range sets.List(sets.KeySet(snippets)) {
			sb.WriteString(fmt.Sprintf(`{{- define "deployment.%s.%s" -}}
%s
{{- end -}}

`, depSpec.Name, fieldName, strings.TrimSpace(snippets[fieldName])))
		}
		sb.WriteString(fmt.Sprintf(`{{- define "deployment.%[1]s.spec.overrides" -}}
  {{- $overrides := dict -}}

  {{- $templateMetadataOverrides := dict
    "annotations" (dict
      "olm.targetNamespaces" (include "olm.targetNamespaces" .)
    )
  -}}

  {{- $templateSpecOverrides := dict -}}
  {{- $origAffinity := fromYaml (include "deployment.%[1]s.affinity" .) -}}
  {{- if .Values.affinity -}}
    {{- $_ := set $templateSpecOverrides "affinity" .Values.affinity -}}
  {{- else if $origAffinity -}}
    {{- $_ := set $templateSpecOverrides "affinity" $origAffinity -}}
  {{- end -}}

  {{- $origNodeSelector := fromYaml (include "deployment.%[1]s.nodeSelector" .) -}}
  {{- if .Values.nodeSelector -}}
    {{- $_ := set $templateSpecOverrides "nodeSelector" .Values.nodeSelector -}}
  {{- else if $origNodeSelector -}}
    {{- $_ := set $templateSpecOverrides "nodeSelector" $origNodeSelector -}}
  {{- end -}}

  {{- $origSelector := fromYaml (include "deployment.%[1]s.selector" .) -}}
  {{- if .Values.selector -}}
    {{- $_ := set $overrides "selector" .Values.selector -}}
  {{- else if $origSelector -}}
    {{- $_ := set $overrides "selector" $origSelector -}}
  {{- end -}}

  {{- $origTolerations := fromYamlArray (include "deployment.%[1]s.tolerations" .) -}}
  {{- if and $origTolerations .Values.tolerations -}}
    {{- $_ := set $templateSpecOverrides "tolerations" (concat $origTolerations .Values.tolerations | uniq) -}}
  {{- else if .Values.tolerations -}}
    {{- $_ := set $templateSpecOverrides "tolerations" .Values.tolerations -}}
  {{- else if $origTolerations -}}
    {{- $_ := set $templateSpecOverrides "tolerations" $origTolerations -}}
  {{- end -}}

  {{- $origVolumes := fromYamlArray (include "deployment.%[1]s.volumes" .) -}}
  {{- if and $origVolumes .Values.volumes -}}
    {{- $volumes := .Values.volumes -}}
    {{- $volumeNames := list -}}
    {{- range $volumes -}}{{- $volumeNames = append $volumeNames .name -}}{{- end -}}
    {{- range $origVolumes -}}
      {{- if not (has .name $volumeNames) -}}
        {{- $volumes = append $volumes . -}}
        {{- $volumeNames = append $volumeNames .name -}}
      {{- end -}}
    {{- end -}}
    {{- $_ := set $templateSpecOverrides "volumes" $volumes -}}
  {{- else if .Values.volumes -}}
    {{- $_ := set $templateSpecOverrides "volumes" .Values.volumes -}}
  {{- else if $origVolumes -}}
    {{- $_ := set $templateSpecOverrides "volumes" $origVolumes -}}
  {{- end -}}

  {{- $containers := list -}}
`, depSpec.Name))

		for i, container := range depSpec.Spec.Template.Spec.Containers {
			sb.WriteString(fmt.Sprintf(`

  {{- $origContainer%[1]d := fromYaml (include "deployment.%[2]s.%[3]s" .) -}}

  {{- $origEnv%[1]d := $origContainer%[1]d.env -}}
  {{- if and $origEnv%[1]d .Values.env -}}
    {{- $env := .Values.env -}}
    {{- $envNames := list -}}
    {{- range $env -}}{{- $envNames = append $envNames .name -}}{{- end -}}
    {{- range $origEnv%[1]d -}}
      {{- if not (has .name $envNames) -}}
        {{- $env = append $env . -}}
        {{- $envNames = append $envNames .name -}}
      {{- end -}}
    {{- end -}}
    {{- $_ := set $origContainer%[1]d "env" $env -}}
  {{- else if .Values.env -}}
    {{- $_ := set $origContainer%[1]d "env" .Values.env -}}
  {{- end -}}

  {{- $origEnvFrom%[1]d := $origContainer%[1]d.envFrom -}}
  {{- if and $origEnvFrom%[1]d .Values.envFrom -}}
    {{- $_ := set $origContainer%[1]d "envFrom" (concat $origEnvFrom%[1]d .Values.envFrom | uniq) -}}
  {{- else if .Values.envFrom -}}
    {{- $_ := set $origContainer%[1]d "envFrom" .Values.envFrom -}}
  {{- end -}}

  {{- $origResources%[1]d := $origContainer%[1]d.resources -}}
  {{- if .Values.resources -}}
    {{- $_ := set $origContainer%[1]d "resources" .Values.resources -}}
  {{- end -}}

  {{- $origVolumeMounts%[1]d := $origContainer%[1]d.volumeMounts -}}
  {{- if and $origVolumeMounts%[1]d .Values.volumeMounts -}}
    {{- $volumeMounts := .Values.volumeMounts -}}
    {{- $volumeMountNames := list -}}
    {{- range $volumeMounts -}}{{- $volumeMountNames = append $volumeMountNames .name -}}{{- end -}}
    {{- range $origVolumeMounts%[1]d -}}
      {{- if not (has .name $volumeMountNames) -}}
        {{- $volumeMounts = append $volumeMounts . -}}
        {{- $volumeMountNames = append $volumeMountNames .name -}}
      {{- end -}}
    {{- end -}}
    {{- $_ := set $origContainer%[1]d "volumeMounts" $volumeMounts -}}
  {{- else if .Values.volumeMounts -}}
    {{- $_ := set $origContainer%[1]d "volumeMounts" .Values.volumeMounts -}}
  {{- end -}}

  {{- $containers = append $containers $origContainer%[1]d -}}`, i, depSpec.Name, container.Name))
		}

		sb.WriteString(`
  {{- $templateSpecOverrides := merge $templateSpecOverrides (dict "containers" $containers) -}}

  {{- $overrides = merge $overrides (dict "template" (dict "metadata" $templateMetadataOverrides)) -}}
  {{- $overrides = merge $overrides (dict "template" (dict "spec" $templateSpecOverrides)) -}}
  {{- dict "spec" $overrides | toYaml -}}
{{- end -}}`)
	}
	return &chart.File{
		Name: "templates/_helpers.deployments.tpl",
		Data: sb.Bytes(),
	}
}

func newTargetNamespacesTemplateHelper(watchNsSetup watchNamespaceSchemaConfig) *chart.File {
	filename := "templates/_helpers.targetNamespaces.tpl"
	defineTemplate := `{{- define "olm.targetNamespaces" -}}
%s
{{- end -}}
`

	if !watchNsSetup.IncludeField {
		templateHelperDefaultValue := fmt.Sprintf(`{{- %s -}}`, watchNsSetup.TemplateHelperDefaultValue)
		return &chart.File{
			Name: filename,
			Data: []byte(fmt.Sprintf(defineTemplate, templateHelperDefaultValue)),
		}
	}

	switch watchNsSetup.FieldName {
	case "installMode":
		value := `{{- $installMode := .Values.installMode -}}`
		if !watchNsSetup.Required {
			value += fmt.Sprintf(`
{{- if not $installMode -}}
  {{- $installMode = %s -}}
{{- end -}}`, watchNsSetup.TemplateHelperDefaultValue)
		}
		value += fmt.Sprintf(`
{{- if eq $installMode "AllNamespaces" -}}
  {{- "" -}}
{{- else if eq $installMode "OwnNamespace" -}}
  {{- .Release.Namespace -}}
{{- else -}}
  {{- fail (printf "Unsupported install mode: %%s" $installMode) -}}
{{- end -}}`)

		return &chart.File{
			Name: filename,
			Data: []byte(fmt.Sprintf(defineTemplate, value)),
		}
	case "watchNamespace":
		value := `{{- $targetNamespaces := .Values.watchNamespace -}}`
		if !watchNsSetup.Required {
			value += fmt.Sprintf(`
{{- if not $targetNamespaces -}}
  {{- $targetNamespaces = %s -}}
{{- end -}}`, watchNsSetup.TemplateHelperDefaultValue)
		}
		if !watchNsSetup.AllowReleaseNamespace {
			value += `
{{- if eq $targetNamespaces .Release.Namespace -}}
  {{- fail "OwnNamespace mode is not supported. watchNamespace cannot be set to the install namespace" -}}
{{- end -}}`
		}
		value += `
{{- $targetNamespaces -}}`
		return &chart.File{
			Name: filename,
			Data: []byte(fmt.Sprintf(defineTemplate, value)),
		}
	case "watchNamespaces":
		value := `{{- $targetNamespaces := .Values.watchNamespaces -}}`
		if !watchNsSetup.Required {
			value += fmt.Sprintf(`
{{- if not $targetNamespaces -}}
  {{- $targetNamespaces = %s -}}
{{- end -}}`, watchNsSetup.TemplateHelperDefaultValue)
		}
		if !watchNsSetup.AllowReleaseNamespace {
			value += `
{{- if has .Release.Namespace $targetNamespaces -}}
  {{- fail "OwnNamespace mode is not supported. watchNamespaces cannot include the install namespace" -}}
{{- end -}}`
		}
		value += `
{{- join "," $targetNamespaces -}}`
		return &chart.File{
			Name: filename,
			Data: []byte(fmt.Sprintf(defineTemplate, value)),
		}
	}

	return nil
}

func newDeploymentFiles(csv v1alpha1.ClusterServiceVersion) ([]*chart.File, error) {
	var (
		files = make([]*chart.File, 0, len(csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs))
		errs  []error
	)

	csvMetadataAnnotations := csv.GetAnnotations()
	for _, depSpec := range csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		dep := appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:   depSpec.Name,
				Labels: depSpec.Label,
			},
			Spec: depSpec.Spec,
		}
		dep.SetGroupVersionKind(appsv1.SchemeGroupVersion.WithKind("Deployment"))
		dep.Spec.Template.SetAnnotations(mergeMaps(dep.Spec.Template.Annotations, csvMetadataAnnotations))
		delete(dep.Spec.Template.Annotations, "olm.targetNamespaces")

		// Hardcode the deployment with RevisionHistoryLimit=1 (something OLMv0 does, not sure why)
		dep.Spec.RevisionHistoryLimit = ptr.To(int32(1))

		dep.Spec.Template.Spec.Affinity = nil
		dep.Spec.Template.Spec.NodeSelector = nil
		dep.Spec.Selector = nil
		dep.Spec.Template.Spec.Tolerations = nil
		dep.Spec.Template.Spec.Volumes = nil

		for i := range dep.Spec.Template.Spec.Containers {
			dep.Spec.Template.Spec.Containers[i].Env = nil
			dep.Spec.Template.Spec.Containers[i].EnvFrom = nil
			dep.Spec.Template.Spec.Containers[i].Resources = corev1.ResourceRequirements{}
			dep.Spec.Template.Spec.Containers[i].VolumeMounts = nil
		}

		depFile, err := newFile(&dep, parametrize.MergeBlock(fmt.Sprintf(`fromYaml (include "deployment.%s.spec.overrides" .)`, dep.Name), "spec"))
		if err != nil {
			errs = append(errs, fmt.Errorf("create template file for deployment %q: %w", depSpec.Name, err))
			continue
		}
		files = append(files, depFile)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return files, nil
}

func fileNameForObject(gk schema.GroupKind, name string) string {
	if gk.Group == "" {
		gk.Group = "core"
	}
	return fmt.Sprintf("templates/%s.%s-%s.yaml", gk.Group, gk.Kind, name)
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

func newValuesSchemaFile(watchNsConfig watchNamespaceSchemaConfig) ([]byte, error) {
	sch, err := getSchema(watchNsConfig)
	if err != nil {
		return nil, err
	}

	var jsonDataBuf bytes.Buffer
	enc := json.NewEncoder(&jsonDataBuf)
	enc.SetIndent("", "    ")
	if err := enc.Encode(sch); err != nil {
		return nil, err
	}
	return jsonDataBuf.Bytes(), nil
}

type watchNamespaceSchemaConfig struct {
	IncludeField               bool
	FieldName                  string
	Required                   bool
	Schema                     *spec.Schema
	TemplateHelperDefaultValue string
	AllowReleaseNamespace      bool
}

const watchNamespacePattern = `^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`

// watchNamespacesSchemaProperties return the OpenAPI v3 schema for the field that controls the namespace or namespaces to watch.
// It returns the schema as a byte slice, a boolean indicating if the field is required. If the returned byte slice is nil,
// the field should not be included in the schema.
func getWatchNamespacesSchema(installModes []v1alpha1.InstallMode) watchNamespaceSchemaConfig {
	const (
		AllNamespaces   = 1 << iota // 1 (0001)
		OwnNamespace                // 2 (0010)
		SingleNamespace             // 4 (0100)
		MultiNamespace              // 8 (1000)
	)
	supportedInstallModes := 0
	for _, installMode := range installModes {
		if installMode.Supported {
			switch installMode.Type {
			case v1alpha1.InstallModeTypeAllNamespaces:
				supportedInstallModes |= AllNamespaces
			case v1alpha1.InstallModeTypeOwnNamespace:
				supportedInstallModes |= OwnNamespace
			case v1alpha1.InstallModeTypeSingleNamespace:
				supportedInstallModes |= SingleNamespace
			case v1alpha1.InstallModeTypeMultiNamespace:
				supportedInstallModes |= MultiNamespace
			}
		}
	}

	watchNamespaceSchema := func() *spec.Schema {
		itemSchema := spec.StringProperty()
		itemSchema.Pattern = watchNamespacePattern
		itemSchema.Description = "A namespace that the extension should watch."
		itemSchema.MinLength = ptr.To(int64(1))
		itemSchema.MaxLength = ptr.To(int64(63))
		return itemSchema
	}

	watchNamespacesSchema := func(item *spec.Schema, maxLength int64) *spec.Schema {
		arraySchema := spec.ArrayProperty(item)
		arraySchema.MinItems = ptr.To(int64(1))
		arraySchema.MaxItems = ptr.To(maxLength)
		return arraySchema
	}

	// 16 cases, for each combination of supported install modes
	switch supportedInstallModes {
	case AllNamespaces:
		return watchNamespaceSchemaConfig{
			IncludeField:               false,
			TemplateHelperDefaultValue: `""`,
		}
	case AllNamespaces | OwnNamespace:
		// "installMode" enum
		schema := spec.StringProperty()
		schema.Enum = []interface{}{"AllNamespaces", "OwnNamespace"}

		return watchNamespaceSchemaConfig{
			IncludeField:               true,
			FieldName:                  "installMode",
			Required:                   false,
			Schema:                     schema,
			AllowReleaseNamespace:      true,
			TemplateHelperDefaultValue: `"AllNamespaces"`,
		}
	case AllNamespaces | OwnNamespace | SingleNamespace:
		// "watchNamespace" string, optional, .Release.Namespace allowed, unset means all namespaces
		return watchNamespaceSchemaConfig{
			IncludeField:               true,
			FieldName:                  "watchNamespace",
			Required:                   false,
			Schema:                     watchNamespaceSchema(),
			AllowReleaseNamespace:      true,
			TemplateHelperDefaultValue: `""`,
		}
	case AllNamespaces | OwnNamespace | MultiNamespace, AllNamespaces | OwnNamespace | SingleNamespace | MultiNamespace:
		// "watchNamespaces" array of strings, optional, len(1..10), .Release.Namespace allowed, unset means all namespaces
		return watchNamespaceSchemaConfig{
			IncludeField:               true,
			FieldName:                  "watchNamespaces",
			Required:                   false,
			Schema:                     watchNamespacesSchema(watchNamespaceSchema(), 10),
			AllowReleaseNamespace:      true,
			TemplateHelperDefaultValue: `(list "")`,
		}
	case AllNamespaces | SingleNamespace:
		// "watchNamespace" string, optional, .Release.Namespace not allowed, unset means all namespaces
		return watchNamespaceSchemaConfig{
			IncludeField:               true,
			FieldName:                  "watchNamespace",
			Required:                   false,
			Schema:                     watchNamespaceSchema(),
			AllowReleaseNamespace:      false,
			TemplateHelperDefaultValue: `""`,
		}
	case AllNamespaces | SingleNamespace | MultiNamespace, AllNamespaces | MultiNamespace:
		// "watchNamespaces" array of strings, optional, len(1..10), .Release.Namespace allowed, unset means all namespaces
		return watchNamespaceSchemaConfig{
			IncludeField:               true,
			FieldName:                  "watchNamespaces",
			Required:                   false,
			Schema:                     watchNamespacesSchema(watchNamespaceSchema(), 10),
			AllowReleaseNamespace:      false,
			TemplateHelperDefaultValue: `(list "")`,
		}
	case OwnNamespace:
		// no field
		return watchNamespaceSchemaConfig{
			IncludeField:               false,
			TemplateHelperDefaultValue: `.Release.Namespace`,
		}
	case OwnNamespace | SingleNamespace:
		// "watchNamespace" string, optional, .Release.Namespace allowed, unset means .Release.Namespace
		return watchNamespaceSchemaConfig{
			IncludeField:               true,
			FieldName:                  "watchNamespace",
			Required:                   false,
			Schema:                     watchNamespaceSchema(),
			AllowReleaseNamespace:      true,
			TemplateHelperDefaultValue: `.Release.Namespace`,
		}
	case OwnNamespace | SingleNamespace | MultiNamespace, OwnNamespace | MultiNamespace:
		// "watchNamespaces" array of strings, optional, len(1..10), .Release.Namespace allowed, unset means .Release.Namespace
		return watchNamespaceSchemaConfig{
			IncludeField:               true,
			FieldName:                  "watchNamespaces",
			Required:                   false,
			Schema:                     watchNamespacesSchema(watchNamespaceSchema(), 10),
			AllowReleaseNamespace:      true,
			TemplateHelperDefaultValue: `(list .Release.Namespace)`,
		}
	case SingleNamespace:
		// "watchNamespace" string, required, .Release.Namespace not allowed
		return watchNamespaceSchemaConfig{
			IncludeField:          true,
			FieldName:             "watchNamespace",
			Required:              true,
			Schema:                watchNamespaceSchema(),
			AllowReleaseNamespace: false,
		}
	case SingleNamespace | MultiNamespace, MultiNamespace:
		// "watchNamespaces" array of strings, required, len(1..10), .Release.Namespace not allowed
		return watchNamespaceSchemaConfig{
			IncludeField:          true,
			FieldName:             "watchNamespaces",
			Required:              true,
			Schema:                watchNamespacesSchema(watchNamespaceSchema(), 10),
			AllowReleaseNamespace: false,
		}
	default:
		panic("no supported install modes")
	}
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

//go:embed internal/apis__apps__v1_openapi.json
var appsV1OpenAPI []byte

func getAppsV1DefinitionsFromOpenAPI() (spec.Definitions, error) {
	var docMap map[string]interface{}
	if err := json.Unmarshal(appsV1OpenAPI, &docMap); err != nil {
		return nil, err
	}

	components, ok := docMap["components"].(map[string]interface{})
	if !ok {
		return nil, errors.New("missing components in the Kubernetes API spec")
	}
	schemas, ok := components["schemas"].(map[string]interface{})
	if !ok {
		return nil, errors.New("missing schemas in the Kubernetes API spec")
	}

	jsonSchemas, err := json.Marshal(schemas)
	if err != nil {
		return nil, err
	}

	jsonSchemas = bytes.ReplaceAll(jsonSchemas, []byte("#/components/schemas/"), []byte("#/definitions/"))

	var definitions spec.Definitions
	if err := json.Unmarshal(jsonSchemas, &definitions); err != nil {
		return nil, err
	}
	return definitions, nil
}

func getSchema(watchNsConfig watchNamespaceSchemaConfig) (*spec.Schema, error) {
	definitions, err := getAppsV1DefinitionsFromOpenAPI()
	if err != nil {
		return nil, err
	}

	// Define what we want to find schemas for
	keep := map[string]sets.Set[string]{
		"io.k8s.api.apps.v1.DeploymentSpec":               sets.New[string]("selector"),
		"io.k8s.api.core.v1.PodSpec":                      sets.New[string]("nodeSelector", "tolerations", "volumes", "affinity"),
		"io.k8s.api.core.v1.Container":                    sets.New[string]("resources", "envFrom", "env", "volumeMounts"),
		"io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta": sets.New[string]("annotations"),
	}

	allKeep := map[string]sets.Set[string]{}
	maps.Copy(allKeep, keep)

	// Recursively find all references from the roots
	for defName, properties := range keep {
		for ref := range findRefs(definitions, defName, properties) {
			allKeep[ref] = nil
		}
	}

	// Remove all properties that are not in the keep set
	for defName, def := range definitions {
		keepProps, ok := allKeep[defName]
		if !ok {
			delete(definitions, defName)
			continue
		}
		if keepProps == nil {
			continue
		}
		for propName := range def.Properties {
			if !keepProps.Has(propName) {
				delete(def.Properties, propName)
			}
		}
	}
	properties := spec.SchemaProperties{}
	for defName, keepProps := range keep {
		for propName := range keepProps {
			properties[propName] = *spec.RefProperty(fmt.Sprintf("#/definitions/%s/properties/%s", defName, propName))
		}
	}

	var requiredProperties []string
	if watchNsConfig.IncludeField {
		propName := watchNsConfig.FieldName
		if _, ok := properties[propName]; ok {
			return nil, fmt.Errorf("extra property %q conflicts with existing property", propName)
		}
		properties[propName] = *watchNsConfig.Schema
		if watchNsConfig.Required {
			requiredProperties = append(requiredProperties, propName)
		}
	}

	return &spec.Schema{
		SchemaProps: spec.SchemaProps{
			Schema:               "http://json-schema.org/schema#",
			Type:                 spec.StringOrArray{"object"},
			Properties:           properties,
			Definitions:          definitions,
			Required:             requiredProperties,
			AdditionalProperties: &spec.SchemaOrBool{Allows: false},
		},
	}, nil
}

func findRefs(definitions spec.Definitions, root string, keepProperties sets.Set[string]) sets.Set[string] {
	refs := sets.New[string]()
	definition, ok := definitions[root]
	if !ok {
		return refs
	}
	for propName, prop := range definition.Properties {
		if keepProperties == nil || keepProperties.Has(propName) {
			refs = refs.Union(findRefsInSchema(&prop))
		}
	}
	for ref := range refs {
		refs = refs.Union(findRefs(definitions, ref, nil))
	}
	return refs
}

func findRefsInSchema(sch *spec.Schema) sets.Set[string] {
	refs := sets.New[string]()
	if sch.Ref.String() != "" {
		refs.Insert(strings.TrimPrefix(sch.Ref.String(), "#/definitions/"))
	}
	if sch.Items != nil && sch.Items.Schema != nil {
		refs = refs.Union(findRefsInSchema(sch.Items.Schema))
	}
	if sch.AdditionalProperties != nil && sch.AdditionalProperties.Schema != nil {
		refs = refs.Union(findRefsInSchema(sch.AdditionalProperties.Schema))
	}
	if sch.AdditionalItems != nil && sch.AdditionalItems.Schema != nil {
		refs = refs.Union(findRefsInSchema(sch.AdditionalItems.Schema))
	}
	for _, prop := range sch.Properties {
		refs = refs.Union(findRefsInSchema(&prop))
	}
	for _, item := range sch.AllOf {
		refs = refs.Union(findRefsInSchema(&item))
	}
	for _, item := range sch.AnyOf {
		refs = refs.Union(findRefsInSchema(&item))
	}
	for _, item := range sch.OneOf {
		refs = refs.Union(findRefsInSchema(&item))
	}
	if sch.Not != nil {
		refs = refs.Union(findRefsInSchema(sch.Not))
	}
	return refs
}
