package steps

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

func TestClusterObjectSetSecretAssertionsUseReferences(t *testing.T) {
	originalCLI, originalNamespace := k8sCli, olmNamespace
	originalNamespaces := componentNamespaces
	t.Cleanup(func() {
		k8sCli, olmNamespace = originalCLI, originalNamespace
		componentNamespaces = originalNamespaces
	})
	olmNamespace = "extensions"
	componentNamespaces = map[string]string{objectControllerComponent: "objects"}
	fixtureDir := t.TempDir()
	t.Setenv("E2E_SECRET_FIXTURES", fixtureDir)
	k8sCli = filepath.Join(fixtureDir, "read-fixture")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$E2E_SECRET_FIXTURES/commands"
case "$1 $2" in
  "get clusterobjectset") cat "$E2E_SECRET_FIXTURES/revision.json" ;;
  "get secret") cat "$E2E_SECRET_FIXTURES/$5-$3.json" ;;
  *) printf 'unexpected command: %s\n' "$*" >&2; exit 1 ;;
esac
`
	//nolint:gosec // The private test CLI fixture must be executable.
	require.NoError(t, os.WriteFile(k8sCli, []byte(script), 0o700))
	namespaces := []string{"objects", "extensions", "referenced-content"}
	revision := ocv1.ClusterObjectSet{ObjectMeta: metav1.ObjectMeta{Name: "revision", UID: "revision-uid"}}
	for _, namespace := range namespaces {
		revision.Spec.Phases = append(revision.Spec.Phases, ocv1.ClusterObjectSetPhase{
			Name: namespace,
			Objects: []ocv1.ClusterObjectSetObject{
				{Ref: ocv1.ObjectSourceRef{Name: "manifest", Namespace: namespace, Key: "first"}},
				{Ref: ocv1.ObjectSourceRef{Name: "manifest", Namespace: namespace, Key: "second"}},
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
		require.NoError(t, os.WriteFile(filepath.Join(fixtureDir, namespace+"-manifest.json"), data, 0o600))
	}
	revision.Spec.Phases = append(revision.Spec.Phases, ocv1.ClusterObjectSetPhase{
		Name: "implicit-namespace",
		Objects: []ocv1.ClusterObjectSetObject{
			{Ref: ocv1.ObjectSourceRef{Name: "manifest", Key: "implicit"}},
		},
	})
	data, err := json.Marshal(revision)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(fixtureDir, "revision.json"), data, 0o600))
	suite := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			sc.Before(CreateScenarioContext)
			RegisterObjectControllerSteps(sc)
		},
		Options: &godog.Options{
			Format: "progress", Output: io.Discard, TestingT: t, Strict: true,
			FeatureContents: []godog.Feature{{Name: "secret_refs.feature", Contents: []byte(`Feature: Referenced Secrets
  Scenario: Secrets can be stored independently of controllers
    Given ClusterObjectSet "revision" referred secrets are immutable
    And ClusterObjectSet "revision" referred secrets contain labels
      | key                                    | value    |
      | olm.operatorframework.io/revision-name | revision |
    And ClusterObjectSet "revision" referred secrets are owned by the object set
    And ClusterObjectSet "revision" referred secrets have type "olm.operatorframework.io/object-data"
`)}},
		},
	}
	require.Zero(t, suite.Run())
	commands, err := os.ReadFile(filepath.Join(fixtureDir, "commands"))
	require.NoError(t, err)
	for _, namespace := range namespaces {
		// Each assertion must fetch a Secret once, even when two objects reference its data.
		require.Equal(t, 4, strings.Count(string(commands), "get secret manifest -n "+namespace+" -o json\n"))
	}
}

func TestResolveObjectRefNamespaceDefault(t *testing.T) {
	originalCLI, originalNamespace := k8sCli, olmNamespace
	t.Cleanup(func() {
		k8sCli, olmNamespace = originalCLI, originalNamespace
	})
	olmNamespace = "extensions"
	fixtureDir := t.TempDir()
	t.Setenv("E2E_REF_FIXTURES", fixtureDir)
	k8sCli = filepath.Join(fixtureDir, "read-ref")
	script := "#!/bin/sh\nnamespace=${5:-kubeconfig-default}\ncat \"$E2E_REF_FIXTURES/$namespace.json\"\n"
	//nolint:gosec // The private test CLI fixture must be executable.
	require.NoError(t, os.WriteFile(k8sCli, []byte(script), 0o700))
	for _, namespace := range []string{"extensions", "objects", "kubeconfig-default"} {
		manifest := []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"from-` + namespace + `"}}`)
		secret := corev1.Secret{Data: map[string][]byte{"object": manifest}}
		data, err := json.Marshal(secret)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(fixtureDir, namespace+".json"), data, 0o600))
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
			object, err := resolveObjectRef(ocv1.ObjectSourceRef{Name: "manifest", Namespace: tc.namespace, Key: "object"})
			require.NoError(t, err)
			require.Equal(t, "from-"+tc.want, object.GetName())
		})
	}
}

func TestObjectControllerReadinessUsesDiscoveredNamespace(t *testing.T) {
	originalGates, originalNamespaces := featureGates, componentNamespaces
	originalNamespace, originalOnly, originalCLI := olmNamespace, objectControllerOnly, k8sCli
	t.Cleanup(func() {
		featureGates, componentNamespaces = originalGates, originalNamespaces
		olmNamespace, objectControllerOnly, k8sCli = originalNamespace, originalOnly, originalCLI
	})
	k8sCli = filepath.Join(t.TempDir(), "capture-command")
	//nolint:gosec // The private test CLI fixture must be executable.
	require.NoError(t, os.WriteFile(k8sCli, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$E2E_READINESS_COMMANDS\"\n"), 0o700))
	for _, tc := range []struct {
		name       string
		present    bool
		standalone bool
	}{
		{name: "missing standalone deployment", standalone: true},
		{name: "missing integrated deployment"},
		{name: "standalone namespace", present: true, standalone: true},
		{name: "integrated namespace", present: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			featureGates = maps.Clone(originalGates)
			// A namespace from an earlier discovery must be replaced or cleared.
			componentNamespaces = map[string]string{objectControllerComponent: "previous-installation"}
			olmNamespace, objectControllerOnly = "extensions", tc.standalone
			deployments := map[string]*appsv1.Deployment{}
			if tc.present {
				deployments[objectControllerComponent] = &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "objects"}}
			}
			configureControllerFeatures(deployments)
			commandsPath := filepath.Join(t.TempDir(), "commands")
			t.Setenv("E2E_READINESS_COMMANDS", commandsPath)
			err := ObjectControllerIsAvailable(t.Context())
			if !tc.present {
				require.ErrorContains(t, err, "object-controller is not installed")
				require.ErrorContains(t, err, objectControllerDeploymentName)
				require.NoFileExists(t, commandsPath)
				return
			}
			require.NoError(t, err)
			commands, err := os.ReadFile(commandsPath)
			require.NoError(t, err)
			require.Contains(t, string(commands), "rollout status deployment/"+objectControllerDeploymentName+" -n objects --timeout=")
		})
	}
}

func TestServiceAccountPermissionsUseScenarioTags(t *testing.T) {
	originalGates, originalCLI := featureGates, k8sCli
	t.Cleanup(func() {
		featureGates, k8sCli = originalGates, originalCLI
	})
	k8sCli = filepath.Join(t.TempDir(), "capture-manifest")
	//nolint:gosec // The private test CLI fixture must be executable.
	require.NoError(t, os.WriteFile(k8sCli, []byte("#!/bin/sh\ncat > \"$E2E_RBAC_MANIFEST\"\n"), 0o700))

	for _, tc := range []struct {
		name        string
		featureFile string
		tag         string
		boxcutter   bool
		wantHelm    bool
	}{
		{name: "renamed direct feature", featureFile: "renamed.feature", tag: "@ObjectController"},
		{name: "feature name alone does not select boxcutter", featureFile: "revision.feature", wantHelm: true},
		{name: "unrelated tag does not select boxcutter", featureFile: "renamed.feature", tag: "@ObjectControllerMetrics", wantHelm: true},
		{name: "integrated boxcutter runtime", featureFile: "install.feature", boxcutter: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			featureGates = maps.Clone(originalGates)
			featureGates[features.BoxcutterRuntime] = tc.boxcutter
			manifestPath := filepath.Join(t.TempDir(), "manifest.yaml")
			t.Setenv("E2E_RBAC_MANIFEST", manifestPath)
			scenario := &godog.Scenario{Id: "permissions", Uri: tc.featureFile}
			if tc.tag != "" {
				require.NoError(t, json.Unmarshal([]byte(`{"tags":[{"name":"`+tc.tag+`"}]}`), scenario))
			}
			ctx, err := CreateScenarioContext(t.Context(), scenario)
			require.NoError(t, err)
			err = ServiceAccountWithNeededPermissionsIsAvailableInTestNamespace(ctx, "pvc-probe-sa")
			if tc.wantHelm {
				// This account only has a Boxcutter template, so selecting Helm must fail.
				require.ErrorContains(t, err, "pvc-probe-sa-helm-rbac-template.yaml")
				return
			}
			require.NoError(t, err)
			manifest, err := os.ReadFile(manifestPath)
			require.NoError(t, err)
			require.Contains(t, string(manifest), "resources: [clusterobjectsets/finalizers]")
			require.Contains(t, string(manifest), "name: ns-permissions-pvc-probe-sa-clusterrole")
		})
	}
}

func TestObjectControllerScenarioSelection(t *testing.T) {
	originalGates, originalNamespaces := featureGates, componentNamespaces
	originalNamespace, originalOnly := olmNamespace, objectControllerOnly
	t.Cleanup(func() {
		featureGates, componentNamespaces = originalGates, originalNamespaces
		olmNamespace, objectControllerOnly = originalNamespace, originalOnly
	})
	for _, tc := range []struct {
		name          string
		objectPresent bool
		operator      bool
		boxcutter     bool
		standalone    bool
		wantSkip      bool
	}{
		{name: "standalone", objectPresent: true, standalone: true},
		{name: "standard", operator: true, wantSkip: true},
		{name: "integrated", objectPresent: true, operator: true, boxcutter: true},
		{name: "unready deployment must run readiness check", objectPresent: true},
		{name: "missing integrated controller must fail readiness", operator: true, boxcutter: true},
		{name: "missing standalone controller must fail readiness", standalone: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			featureGates = maps.Clone(originalGates)
			componentNamespaces = map[string]string{}
			objectControllerOnly = tc.standalone
			deployments := map[string]*appsv1.Deployment{}
			if tc.objectPresent {
				// Discovery must enable scenarios even before the Deployment is ready.
				deployments[objectControllerComponent] = &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "objects"}}
			}
			if tc.operator {
				deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "extensions"}}
				deployment.Spec.Template.Spec.Containers = []corev1.Container{{Name: "manager", Args: []string{"--feature-gates=BoxcutterRuntime=false"}}}
				if tc.boxcutter {
					deployment.Spec.Template.Spec.Containers[0].Args = []string{"--feature-gates=BoxcutterRuntime=true"}
				}
				deployments[operatorControllerComponent] = deployment
			}
			configureControllerFeatures(deployments)
			require.Equal(t, tc.objectPresent, featureGates[objectControllerFeature])
			require.Equal(t, tc.boxcutter, featureGates[features.BoxcutterRuntime])
			if tc.objectPresent {
				require.Equal(t, "objects", namespaceForComponent(objectControllerComponent))
			}
			scenario := &godog.Scenario{}
			require.NoError(t, json.Unmarshal([]byte(`{"tags":[{"name":"@ObjectController"}]}`), scenario))
			_, err := CheckFeatureTags(context.Background(), scenario)
			if tc.wantSkip {
				require.ErrorIs(t, err, godog.ErrSkip)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
