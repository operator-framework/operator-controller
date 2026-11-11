package steps

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	shared "github.com/operator-framework/operator-controller/test/internal/e2e/steps"
)

// TestObjectControllerReadiness checks API creation, establishment, controller
// availability, and isolation for standalone and integrated scenarios.
func TestObjectControllerReadiness(t *testing.T) {
	for _, tc := range []struct {
		name         string
		standalone   bool
		missing      bool
		integrated   bool
		cod          bool
		delayedCRDs  bool
		forbiddenAPI string
		failCommand  string
		wantError    string
	}{
		{name: "standalone COS", standalone: true},
		{name: "standalone COD", standalone: true, cod: true},
		{name: "integrated namespaces", integrated: true},
		{name: "COS API created after discovery", delayedCRDs: true},
		{name: "COS and COD APIs created after discovery", cod: true, delayedCRDs: true},
		{name: "missing standalone deployment", standalone: true, missing: true, wantError: "object-controller is not installed"},
		{name: "missing integrated deployment", integrated: true, missing: true, wantError: "object-controller is not installed"},
		{name: "operator deployment violates isolation", standalone: true, integrated: true, wantError: "require operator-controller and catalogd to be absent"},
		{name: "ClusterExtension violates isolation", standalone: true, forbiddenAPI: "clusterextensions", wantError: "APIs to be absent"},
		{name: "ClusterCatalog violates isolation", standalone: true, forbiddenAPI: "clustercatalogs", wantError: "APIs to be absent"},
		{name: "isolation check errors fail closed", standalone: true, failCommand: "get crd", wantError: "checking standalone API isolation"},
		{name: "COS API creation is required", failCommand: "wait --for=create crd/clusterobjectsets.", wantError: "clusterobjectsets.olm.operatorframework.io was not created"},
		{name: "COD API creation is required for COD", cod: true, failCommand: "wait --for=create crd/clusterobjectdeployments.", wantError: "clusterobjectdeployments.olm.operatorframework.io was not created"},
		{name: "COS API is required", failCommand: "wait --for=condition=Established crd/clusterobjectsets.", wantError: "clusterobjectsets.olm.operatorframework.io is unavailable"},
		{name: "COD API is required for COD", cod: true, failCommand: "wait --for=condition=Established crd/clusterobjectdeployments.", wantError: "clusterobjectdeployments.olm.operatorframework.io is unavailable"},
		{name: "COS does not require COD creation", failCommand: "wait --for=create crd/clusterobjectdeployments."},
		{name: "COS does not require COD", failCommand: "wait --for=condition=Established crd/clusterobjectdeployments."},
		{name: "unavailable controller fails", failCommand: "rollout status", wantError: "object-controller is unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldNamespaces := shared.ComponentNamespaces
			oldCLI := pflag.Lookup("k8s.cli").Value.String()
			oldOnly := pflag.Lookup("e2e.object-controller-only").Value.String()
			t.Cleanup(func() {
				shared.ComponentNamespaces = oldNamespaces
				require.NoError(t, pflag.Set("k8s.cli", oldCLI))
				require.NoError(t, pflag.Set("e2e.object-controller-only", oldOnly))
			})
			shared.ComponentNamespaces = map[string]string{}
			if !tc.missing {
				shared.ComponentNamespaces[shared.ObjectControllerComponent] = "objects"
			}
			if tc.integrated {
				shared.ComponentNamespaces[shared.OperatorControllerComponent] = "extensions"
				shared.ComponentNamespaces[shared.CatalogdComponent] = "catalogs"
			}
			require.NoError(t, pflag.Set("e2e.object-controller-only", map[bool]string{true: "true", false: "false"}[tc.standalone]))
			dir := t.TempDir()
			cli, commands := filepath.Join(dir, "kubectl"), filepath.Join(dir, "commands")
			t.Setenv("E2E_READINESS_COMMANDS", commands)
			t.Setenv("E2E_FORBIDDEN_API", tc.forbiddenAPI)
			t.Setenv("E2E_FAIL_COMMAND", tc.failCommand)
			t.Setenv("E2E_DELAYED_CRDS", "")
			if tc.delayedCRDs {
				t.Setenv("E2E_DELAYED_CRDS", dir)
			}
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$E2E_READINESS_COMMANDS"
if [ -n "$E2E_FAIL_COMMAND" ]; then
  case "$*" in "$E2E_FAIL_COMMAND"*) echo "not available" >&2; exit 1;; esac
fi
if [ -n "$E2E_DELAYED_CRDS" ]; then
  case "$1 $2" in
    "wait --for=create")
      # Simulate a CRD appearing during the creation wait.
      touch "$E2E_DELAYED_CRDS/${3#crd/}";;
    "wait --for=condition=Established")
      if [ ! -f "$E2E_DELAYED_CRDS/${3#crd/}" ]; then
        echo "CRD not found" >&2
        exit 1
      fi;;
  esac
fi
case "$1 $2" in "get crd") printf '%s' "$E2E_FORBIDDEN_API";; esac
`
			//nolint:gosec // The private test CLI must be executable.
			require.NoError(t, os.WriteFile(cli, []byte(script), 0o700))
			require.NoError(t, pflag.Set("k8s.cli", cli))
			scenario := &godog.Scenario{Id: "1", Uri: "renamed.feature"}
			if tc.cod {
				require.NoError(t, json.Unmarshal([]byte(`{"tags":[{"name":"@ClusterObjectDeployment"}]}`), scenario))
			}
			ctx, err := shared.CreateScenarioContext(t.Context(), scenario)
			require.NoError(t, err)
			err = ObjectControllerIsAvailable(ctx)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
			}
			if tc.missing || tc.standalone && tc.integrated {
				require.NoFileExists(t, commands, "discovery errors should fail immediately")
				return
			}
			data, err := os.ReadFile(commands)
			require.NoError(t, err)
			if strings.HasPrefix(tc.failCommand, "wait --for=create ") && tc.wantError != "" {
				api := strings.TrimPrefix(tc.failCommand, "wait --for=create ")
				require.NotContains(t, string(data), "wait --for=condition=Established "+api)
				require.NotContains(t, string(data), "rollout status")
			}
			if tc.wantError == "" {
				require.Contains(t, string(data), "rollout status deployment/"+shared.ObjectControllerDeploymentName+" -n objects --timeout=")
				require.Equal(t, tc.cod, strings.Contains(string(data), "crd/clusterobjectdeployments."))
				require.Equal(t, tc.standalone, strings.Contains(string(data), "get crd clusterextensions."))
			}
		})
	}
}

// TestReferredSecretAssertionsUseReferenceNamespaces checks that Secret assertions
// use declared namespaces and deduplicate implicit system-namespace references.
func TestReferredSecretAssertionsUseReferenceNamespaces(t *testing.T) {
	oldNamespaces, oldNamespace, oldCLI := shared.ComponentNamespaces, shared.OLMNamespace, pflag.Lookup("k8s.cli").Value.String()
	t.Cleanup(func() {
		shared.ComponentNamespaces, shared.OLMNamespace = oldNamespaces, oldNamespace
		require.NoError(t, pflag.Set("k8s.cli", oldCLI))
	})
	shared.OLMNamespace = "extensions"
	shared.ComponentNamespaces = map[string]string{shared.ObjectControllerComponent: "objects"}
	dir := t.TempDir()
	t.Setenv("E2E_SECRET_FIXTURES", dir)
	cli := filepath.Join(dir, "kubectl")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$E2E_SECRET_FIXTURES/commands"
case "$1 $2" in
  "get clusterobjectset") cat "$E2E_SECRET_FIXTURES/revision.json";;
  "get secret") cat "$E2E_SECRET_FIXTURES/${5:-kubeconfig-default}-$3.json";;
  *) echo "unexpected command: $*" >&2; exit 1;;
esac
`
	//nolint:gosec // The private test CLI must be executable.
	require.NoError(t, os.WriteFile(cli, []byte(script), 0o700))
	require.NoError(t, pflag.Set("k8s.cli", cli))
	revision := ocv1.ClusterObjectSet{ObjectMeta: metav1.ObjectMeta{Name: "revision", UID: "revision-uid"}}
	namespaces := []string{"objects", "extensions", "referenced-content"}
	for _, namespace := range namespaces {
		revision.Spec.Phases = append(revision.Spec.Phases, ocv1.ClusterObjectSetPhase{
			Name: namespace,
			Objects: []ocv1.ClusterObjectSetObject{
				{Type: ocv1.ObjectTypeSecretReference, SecretRef: ocv1.SecretSourceRef{Name: "manifest", Namespace: namespace, Key: "first"}},
				{Type: ocv1.ObjectTypeSecretReference, SecretRef: ocv1.SecretSourceRef{Name: "manifest", Namespace: namespace, Key: "second"}},
			},
		})
		secret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "manifest", Namespace: namespace,
				Labels: map[string]string{"olm.operatorframework.io/revision-name": revision.Name},
				OwnerReferences: []metav1.OwnerReference{{
					Kind: ocv1.ClusterObjectSetKind, Name: revision.Name, UID: revision.UID, Controller: ptr.To(true),
				}},
			},
			Immutable: ptr.To(true),
			Type:      "olm.operatorframework.io/object-data",
		}
		data, err := json.Marshal(secret)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, namespace+"-manifest.json"), data, 0o600))
	}
	// The omitted namespace must deduplicate with the explicit system namespace.
	revision.Spec.Phases = append(revision.Spec.Phases, ocv1.ClusterObjectSetPhase{
		Name: "implicit",
		Objects: []ocv1.ClusterObjectSetObject{{
			Type: ocv1.ObjectTypeSecretReference, SecretRef: ocv1.SecretSourceRef{Name: "manifest", Key: "implicit"},
		}},
	})
	data, err := json.Marshal(revision)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "revision.json"), data, 0o600))
	suite := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			sc.Before(shared.CreateScenarioContext)
			RegisterSteps(sc)
		},
		Options: &godog.Options{
			Format: "progress", Output: io.Discard, TestingT: t, Strict: true,
			FeatureContents: []godog.Feature{{Name: "renamed.feature", Contents: []byte(`Feature: Secret reference namespaces
  Scenario: Assertions use each reference's namespace
    Given ClusterObjectSet "revision" referred secrets are immutable
    And ClusterObjectSet "revision" referred secrets contain labels
      | key                                   | value    |
      | olm.operatorframework.io/revision-name | revision |
    And ClusterObjectSet "revision" referred secrets are owned by the object set
    And ClusterObjectSet "revision" referred secrets have type "olm.operatorframework.io/object-data"
`)}},
		},
	}
	require.Zero(t, suite.Run())
	commands, err := os.ReadFile(filepath.Join(dir, "commands"))
	require.NoError(t, err)
	for _, namespace := range namespaces {
		require.Equal(t, 4, strings.Count(string(commands), "get secret manifest -n "+namespace+" -o json\n"))
	}
}
