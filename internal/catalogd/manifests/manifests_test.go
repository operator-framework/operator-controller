package manifests_test

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// TestCatalogdRetiredCatalogs checks that only OpenShift renders configure catalogd to delete
// the retired openshift-redhat-marketplace ClusterCatalog, and that they grant delete on
// ClusterCatalogs for that name only. Other renders keep their existing behavior.
func TestCatalogdRetiredCatalogs(t *testing.T) {
	allVerbs := []string{"create", "delete", "get", "list", "patch", "update", "watch"}
	allVerbsExceptDelete := []string{"create", "get", "list", "patch", "update", "watch"}

	for _, tc := range []struct {
		name   string
		values string
		// wantRetiredArgs are the --retired-catalogs arguments of the catalogd manager container.
		wantRetiredArgs []string
		// wantUnnamedVerbs are the verbs granted on all ClusterCatalogs.
		wantUnnamedVerbs []string
		// wantDeleteResourceNames are the names of the ClusterCatalogs that may be deleted
		// through a rule restricted by resourceNames.
		wantDeleteResourceNames []string
	}{
		{
			name:             "standard",
			values:           `{}`,
			wantUnnamedVerbs: allVerbs,
		},
		{
			name:             "standard with cert-manager",
			values:           `options: {certManager: {enabled: true}}`,
			wantUnnamedVerbs: allVerbs,
		},
		{
			name:             "experimental with cert-manager and e2e",
			values:           `options: {featureSet: experimental, certManager: {enabled: true}, e2e: {enabled: true}}`,
			wantUnnamedVerbs: allVerbs,
		},
		{
			name:                    "openshift",
			values:                  `options: {openshift: {enabled: true}}`,
			wantRetiredArgs:         []string{"--retired-catalogs=openshift-redhat-marketplace"},
			wantUnnamedVerbs:        allVerbsExceptDelete,
			wantDeleteResourceNames: []string{"openshift-redhat-marketplace"},
		},
		{
			name:                    "openshift catalogd only",
			values:                  `options: {openshift: {enabled: true}, operatorController: {enabled: false}, catalogd: {podDisruptionBudget: {enabled: false}}}`,
			wantRetiredArgs:         []string{"--retired-catalogs=openshift-redhat-marketplace"},
			wantUnnamedVerbs:        allVerbsExceptDelete,
			wantDeleteResourceNames: []string{"openshift-redhat-marketplace"},
		},
		{
			name:                    "openshift experimental catalogd only",
			values:                  `options: {featureSet: experimental, openshift: {enabled: true}, operatorController: {enabled: false}}`,
			wantRetiredArgs:         []string{"--retired-catalogs=openshift-redhat-marketplace"},
			wantUnnamedVerbs:        allVerbsExceptDelete,
			wantDeleteResourceNames: []string{"openshift-redhat-marketplace"},
		},
		{
			name:                    "openshift with cert-manager",
			values:                  `options: {openshift: {enabled: true}, certManager: {enabled: true}}`,
			wantRetiredArgs:         []string{"--retired-catalogs=openshift-redhat-marketplace"},
			wantUnnamedVerbs:        allVerbsExceptDelete,
			wantDeleteResourceNames: []string{"openshift-redhat-marketplace"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects := renderObjects(t, tc.values)

			var deployment appsv1.Deployment
			fromUnstructured(t, objects, "Deployment/catalogd-controller-manager", &deployment)
			var retiredArgs []string
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name != "manager" {
					continue
				}
				for _, arg := range container.Args {
					if strings.HasPrefix(arg, "--retired-catalogs") {
						retiredArgs = append(retiredArgs, arg)
					}
				}
			}
			assert.Equal(t, tc.wantRetiredArgs, retiredArgs, "--retired-catalogs arguments")

			var role rbacv1.ClusterRole
			fromUnstructured(t, objects, "ClusterRole/catalogd-manager-role", &role)
			var unnamedVerbs, deleteResourceNames []string
			for _, rule := range role.Rules {
				if !grants(rule.APIGroups, "olm.operatorframework.io") || !grants(rule.Resources, "clustercatalogs") {
					continue
				}
				if len(rule.ResourceNames) == 0 {
					unnamedVerbs = append(unnamedVerbs, rule.Verbs...)
					continue
				}
				if grants(rule.Verbs, "delete") {
					deleteResourceNames = append(deleteResourceNames, rule.ResourceNames...)
				}
			}
			slices.Sort(unnamedVerbs)
			assert.Equal(t, tc.wantUnnamedVerbs, unnamedVerbs, "verbs granted on all ClusterCatalogs")
			assert.Equal(t, tc.wantDeleteResourceNames, deleteResourceNames, "ClusterCatalogs that may be deleted by name")
		})
	}
}

// grants reports whether an RBAC rule field contains want or the "*" wildcard.
func grants(values []string, want string) bool {
	return slices.Contains(values, want) || slices.Contains(values, "*")
}

func renderObjects(t *testing.T, valuesYAML string) map[string]*unstructured.Unstructured {
	t.Helper()
	chart, err := loader.Load("../../../helm/olmv1")
	require.NoError(t, err)
	values, err := chartutil.ReadValues([]byte(valuesYAML))
	require.NoError(t, err)
	renderValues, err := chartutil.ToRenderValues(chart, values, chartutil.ReleaseOptions{Name: "olmv1"}, nil)
	require.NoError(t, err)
	rendered, err := engine.Render(chart, renderValues)
	require.NoError(t, err)

	objects := map[string]*unstructured.Unstructured{}
	for _, content := range rendered {
		decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(content), 4096)
		for {
			obj := &unstructured.Unstructured{}
			err := decoder.Decode(obj)
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
			if obj.GetKind() != "" {
				objects[obj.GetKind()+"/"+obj.GetName()] = obj
			}
		}
	}
	return objects
}

func fromUnstructured(t *testing.T, objects map[string]*unstructured.Unstructured, key string, into any) {
	t.Helper()
	obj, ok := objects[key]
	require.True(t, ok, "rendered chart is missing %s", key)
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, into))
}
