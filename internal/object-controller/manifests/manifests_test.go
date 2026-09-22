package manifests_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestObjectControllerManifests(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		values                 string
		wantObjectController   bool
		wantOperatorController bool
		wantCatalogd           bool
		wantOpenShift          bool
		wantErr                string
	}{
		{name: "standard", values: `{}`, wantOperatorController: true, wantCatalogd: true},
		{name: "disabled gate takes precedence", values: `options: {featureSet: experimental, operatorController: {features: {enabled: [BoxcutterRuntime], disabled: [BoxcutterRuntime]}}}`, wantOperatorController: true, wantCatalogd: true},
		{name: "experimental with Boxcutter", values: `options: {featureSet: experimental, operatorController: {features: {enabled: [BoxcutterRuntime], disabled: []}}}`, wantObjectController: true, wantOperatorController: true, wantCatalogd: true},
		{name: "standalone", values: `options: {featureSet: experimental, objectController: {enabled: true}, operatorController: {enabled: false}, catalogd: {enabled: false}}`, wantObjectController: true},
		{name: "standalone with cert-manager", values: `options: {featureSet: experimental, objectController: {enabled: true}, operatorController: {enabled: false}, catalogd: {enabled: false}, certManager: {enabled: true}}`, wantObjectController: true},
		{name: "explicitly disabled", values: `options: {featureSet: experimental, objectController: {enabled: false}}`, wantOperatorController: true, wantCatalogd: true},
		{name: "downstream default", values: `options: {openshift: {enabled: true}, catalogd: {enabled: false}}`, wantOperatorController: true, wantOpenShift: true},
		{name: "downstream experimental with Helm", values: `options: {featureSet: experimental, openshift: {enabled: true}, catalogd: {enabled: false}, operatorController: {features: {enabled: [WebhookProviderOpenshiftServiceCA], disabled: [BoxcutterRuntime]}}}`, wantOperatorController: true, wantOpenShift: true},
		{name: "downstream experimental with Boxcutter", values: `options: {featureSet: experimental, openshift: {enabled: true}, catalogd: {enabled: false}, operatorController: {features: {enabled: [BoxcutterRuntime], disabled: []}}}`, wantObjectController: true, wantOperatorController: true, wantOpenShift: true},
		{name: "downstream standalone", values: `options: {featureSet: experimental, openshift: {enabled: true}, objectController: {enabled: true}, operatorController: {enabled: false}, catalogd: {enabled: false}}`, wantObjectController: true, wantOpenShift: true},
		{name: "catalogd only", values: `options: {featureSet: experimental, operatorController: {enabled: false, features: {enabled: [BoxcutterRuntime]}}}`, wantCatalogd: true},
		{name: "standard cannot enable experimental API", values: `options: {objectController: {enabled: true}}`, wantErr: "objectController requires options.featureSet=experimental"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chart, err := loader.Load("../../../helm/olmv1")
			require.NoError(t, err)
			values, err := chartutil.ReadValues([]byte(tc.values))
			require.NoError(t, err)
			renderValues, err := chartutil.ToRenderValues(chart, values, chartutil.ReleaseOptions{Name: "olmv1"}, nil)
			require.NoError(t, err)
			rendered, err := engine.Render(chart, renderValues)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			objects := map[string]*unstructured.Unstructured{}
			seen := map[string]struct{}{}
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
						key := obj.GetKind() + "/" + obj.GetName()
						resourceKey := obj.GetKind() + "/" + obj.GetNamespace() + "/" + obj.GetName()
						require.NotContains(t, seen, resourceKey, "duplicate resource")
						seen[resourceKey] = struct{}{}
						objects[key] = obj
					}
				}
			}
			for name, enabled := range map[string]bool{"object-controller": tc.wantObjectController, "operator-controller": tc.wantOperatorController, "catalogd": tc.wantCatalogd} {
				require.Equal(t, enabled, objects["Deployment/"+name+"-controller-manager"] != nil, name)
			}
			require.Equal(t, tc.wantOperatorController, objects["CustomResourceDefinition/clusterextensions.olm.operatorframework.io"] != nil)
			require.Equal(t, tc.wantCatalogd, objects["CustomResourceDefinition/clustercatalogs.olm.operatorframework.io"] != nil)
			require.Equal(t, tc.wantObjectController, objects["CustomResourceDefinition/clusterobjectsets.olm.operatorframework.io"] != nil)
			for _, resource := range []string{
				"ServiceAccount/object-controller-controller-manager", "ClusterRoleBinding/object-controller-cluster-admin-rolebinding",
				"Role/object-controller-leader-election-role", "RoleBinding/object-controller-leader-election-rolebinding",
				"Service/object-controller-service", "NetworkPolicy/object-controller-controller-manager",
			} {
				require.Equal(t, tc.wantObjectController, objects[resource] != nil, resource)
			}
			if tc.wantObjectController {
				deployment := objects["Deployment/object-controller-controller-manager"]
				sa, _, err := unstructured.NestedString(deployment.Object, "spec", "template", "spec", "serviceAccountName")
				require.NoError(t, err)
				require.Equal(t, "object-controller-controller-manager", sa)
				if tc.wantOpenShift {
					require.Contains(t, objects, "ServiceMonitor/object-controller-metrics-monitor")
					require.Equal(t, "object-controller-cert", objects["Service/object-controller-service"].GetAnnotations()["service.beta.openshift.io/serving-cert-secret-name"])
				}
			}
		})
	}
}
