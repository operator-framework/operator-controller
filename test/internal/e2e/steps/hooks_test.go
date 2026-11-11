package steps

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/component-base/featuregate"
	"k8s.io/utils/ptr"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

// preserveDiscoveryState isolates discovery globals and restores them after the test.
func preserveDiscoveryState(t *testing.T) {
	t.Helper()
	gates, namespaces := FeatureGates, ComponentNamespaces
	namespace, only, cli := OLMNamespace, objectControllerOnly, K8sCLI
	FeatureGates, ComponentNamespaces = maps.Clone(gates), maps.Clone(namespaces)
	t.Cleanup(func() {
		FeatureGates, ComponentNamespaces = gates, namespaces
		OLMNamespace, objectControllerOnly, K8sCLI = namespace, only, cli
	})
}

// fakeCLI replaces the Kubernetes CLI with a temporary executable for the test.
func fakeCLI(t *testing.T, script string) {
	t.Helper()
	K8sCLI = filepath.Join(t.TempDir(), "kubectl")
	//nolint:gosec // The private test CLI must be executable.
	require.NoError(t, os.WriteFile(K8sCLI, []byte("#!/bin/sh\n"+script), 0o700))
}

// taggedScenario constructs a scenario carrying the requested feature tag.
func taggedScenario(t *testing.T, tag string) *godog.Scenario {
	t.Helper()
	scenario := &godog.Scenario{}
	require.NoError(t, json.Unmarshal([]byte(`{"tags":[{"name":"`+tag+`"}]}`), scenario))
	return scenario
}

// TestIndependentControllerDiscovery checks component detection and stale namespace
// cleanup when operator-controller is absent.
func TestIndependentControllerDiscovery(t *testing.T) {
	preserveDiscoveryState(t)
	deployments := []appsv1.Deployment{
		{ObjectMeta: metav1.ObjectMeta{Name: ObjectControllerDeploymentName, Namespace: "objects"}},
		{ObjectMeta: metav1.ObjectMeta{Name: CatalogdDeploymentName, Namespace: "catalogs"}, Spec: appsv1.DeploymentSpec{Replicas: ptr.To(int32(2))}},
		{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "unrelated"}},
	}
	data, err := json.Marshal(deployments)
	require.NoError(t, err)
	t.Setenv("E2E_DEPLOYMENTS", string(data))
	fakeCLI(t, `printf '%s' "$E2E_DEPLOYMENTS"`+"\n")
	detected, err := detectOLMDeployments()
	require.NoError(t, err)
	require.Len(t, detected, 2)
	ComponentNamespaces[OperatorControllerComponent] = "stale"
	configureControllerFeatures(detected)
	require.Equal(t, map[string]string{ObjectControllerComponent: "objects", CatalogdComponent: "catalogs"}, ComponentNamespaces)
	require.Equal(t, "objects", OLMNamespace)
	require.True(t, FeatureGates[ObjectControllerFeature])
	require.True(t, FeatureGates[featuregate.Feature("CatalogdHA")])

	configureControllerFeatures(map[string]*appsv1.Deployment{})
	require.Empty(t, ComponentNamespaces, "removed components must not retain their namespaces")
	require.False(t, FeatureGates[ObjectControllerFeature])
}

// TestObjectControllerScenarioSelection checks that required scenarios reach
// readiness while unrelated scenarios still honor their feature gates.
func TestObjectControllerScenarioSelection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		object     bool
		operator   bool
		boxcutter  bool
		standalone bool
		skip       bool
	}{
		{name: "standalone", object: true, standalone: true},
		{name: "standard skips object scenarios", operator: true, skip: true},
		{name: "integrated", object: true, operator: true, boxcutter: true},
		{name: "unready object deployment still reaches readiness", object: true},
		{name: "missing integrated controller reaches readiness", operator: true, boxcutter: true},
		{name: "missing standalone controller reaches readiness", standalone: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preserveDiscoveryState(t)
			FeatureGates[features.BoxcutterRuntime] = false
			objectControllerOnly = tc.standalone
			deployments := map[string]*appsv1.Deployment{}
			if tc.object {
				deployments[ObjectControllerComponent] = &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "objects"}}
			}
			if tc.operator {
				deployments[OperatorControllerComponent] = &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Namespace: "extensions"},
					Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "manager", Args: []string{
							"--feature-gates=BoxcutterRuntime=" + map[bool]string{false: "false", true: "true"}[tc.boxcutter],
						}}},
					}}},
				}
			}
			configureControllerFeatures(deployments)
			_, err := CheckFeatureTags(t.Context(), taggedScenario(t, "@ObjectController"))
			if tc.skip {
				require.ErrorIs(t, err, godog.ErrSkip)
			} else {
				require.NoError(t, err)
			}
			// Existing integration features still honor the operator's runtime gate.
			_, err = CheckFeatureTags(t.Context(), taggedScenario(t, "@BoxcutterRuntime"))
			if tc.boxcutter {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, godog.ErrSkip)
			}
		})
	}
}

// TestServiceAccountPermissionsUseScenarioTags checks RBAC selection from scenario
// tags and runtime gates regardless of feature filenames.
func TestServiceAccountPermissionsUseScenarioTags(t *testing.T) {
	preserveDiscoveryState(t)
	fakeCLI(t, "cat > \"$E2E_RBAC_MANIFEST\"\n")
	for _, tc := range []struct {
		name      string
		uri       string
		tag       string
		boxcutter bool
		wantHelm  bool
	}{
		{name: "renamed direct feature", uri: "renamed.feature", tag: "@ObjectController"},
		{name: "filename does not select permissions", uri: "revision.feature", wantHelm: true},
		{name: "unrelated tag", uri: "renamed.feature", tag: "@ObjectControllerMetrics", wantHelm: true},
		{name: "integrated boxcutter runtime", uri: "install.feature", boxcutter: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			FeatureGates[features.BoxcutterRuntime] = tc.boxcutter
			output := filepath.Join(t.TempDir(), "rbac.yaml")
			t.Setenv("E2E_RBAC_MANIFEST", output)
			scenario := &godog.Scenario{Id: "permissions", Uri: tc.uri}
			if tc.tag != "" {
				scenario.Tags = taggedScenario(t, tc.tag).Tags
			}
			ctx, err := CreateScenarioContext(t.Context(), scenario)
			require.NoError(t, err)
			err = ServiceAccountWithNeededPermissionsIsAvailableInTestNamespace(ctx, "pvc-probe-sa")
			if tc.wantHelm {
				// This account deliberately has only a Boxcutter template.
				require.ErrorContains(t, err, "pvc-probe-sa-helm-rbac-template.yaml")
				return
			}
			require.NoError(t, err)
			manifest, err := os.ReadFile(output)
			require.NoError(t, err)
			require.Contains(t, string(manifest), "resources: [clusterobjectsets/finalizers]")
			require.Contains(t, string(manifest), "name: "+Scenario(ctx).Namespace+"-pvc-probe-sa-clusterrole")
		})
	}
}

// TestResolveSecretRefNamespaceDefault checks explicit namespaces and the default
// system namespace when resolving Secret references.
func TestResolveSecretRefNamespaceDefault(t *testing.T) {
	preserveDiscoveryState(t)
	OLMNamespace = "extensions"
	dir := t.TempDir()
	t.Setenv("E2E_SECRET_FIXTURES", dir)
	fakeCLI(t, "namespace=${5:-kubeconfig-default}\ncat \"$E2E_SECRET_FIXTURES/$namespace.json\"\n")
	for _, namespace := range []string{"extensions", "objects", "kubeconfig-default"} {
		secret := corev1.Secret{Data: map[string][]byte{
			"object": []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"from-` + namespace + `"}}`),
		}}
		data, err := json.Marshal(secret)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, namespace+".json"), data, 0o600))
	}
	for _, tc := range []struct {
		name      string
		namespace string
		want      string
	}{
		{name: "omitted namespace", want: "extensions"},
		{name: "explicit namespace", namespace: "objects", want: "objects"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj, err := ResolveSecretRef(ocv1.SecretSourceRef{Name: "manifest", Namespace: tc.namespace, Key: "object"})
			require.NoError(t, err)
			require.Equal(t, "from-"+tc.want, obj.GetName())
		})
	}
}
