package steps

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cucumber/godog"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/component-base/featuregate"
	deploymentutil "k8s.io/kubectl/pkg/util/deployment"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
	"github.com/operator-framework/operator-controller/test/internal/catalog"
	testregistry "github.com/operator-framework/operator-controller/test/internal/registry"
)

const (
	// OperatorControllerComponent identifies the ClusterExtension controller.
	OperatorControllerComponent = "operator-controller"
	// CatalogdComponent identifies the catalog controller.
	CatalogdComponent = "catalogd"
	// ObjectControllerComponent identifies the COS/COD controller.
	ObjectControllerComponent = "object-controller"
	// ObjectControllerFeature selects scenarios requiring object-controller.
	ObjectControllerFeature featuregate.Feature = "ObjectController"
	// OperatorControllerDeploymentName is the ClusterExtension controller Deployment.
	OperatorControllerDeploymentName = "operator-controller-controller-manager"
	// CatalogdDeploymentName is the catalog controller Deployment.
	CatalogdDeploymentName = "catalogd-controller-manager"
	// ObjectControllerDeploymentName is the independently deployable controller.
	ObjectControllerDeploymentName = "object-controller-controller-manager"
	defaultTimeout                 = 5 * time.Minute
	// Tick is the polling interval used by wait helpers.
	Tick = 1 * time.Second
)

// Timeout is the per-step wait timeout. It defaults to defaultTimeout (5m) but
// can be overridden via E2E_STEP_TIMEOUT to accommodate runtimes (e.g.
// BoxcutterRuntime) that take longer to complete an installation.
// It is initialized when the package loads; consumers must not mutate it.
var Timeout = func() time.Duration {
	if s := os.Getenv("E2E_STEP_TIMEOUT"); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			return d
		}
	}
	return defaultTimeout
}()

var (
	// OLMNamespace is the OLM system namespace, initialized during suite setup
	// or upgrade installation. Consumers must not mutate it.
	OLMNamespace = "olmv1-system"
	// ComponentNamespaces maps component labels to namespaces discovered by
	// BeforeSuite. Missing entries fall back to OLMNamespace.
	// Consumers must not mutate this map.
	ComponentNamespaces = map[string]string{}
	// KubeconfigPath is configured in init from KUBECONFIG or the kubeconfig flag.
	// Consumers must not mutate it after suite startup.
	KubeconfigPath string
	// K8sCLI is configured in init through the k8s.cli flag.
	// Consumers must not mutate it after suite startup.
	K8sCLI              string
	deployImageRegistry = sync.OnceValue(func() error {
		// Only deploy the registry on kind clusters
		providerID, err := K8sClient(context.Background(), "get", "nodes", "-o", "jsonpath={.items[0].spec.providerID}")
		if err != nil || !strings.HasPrefix(providerID, "kind://") {
			return nil
		}

		cfg, err := ctrl.GetConfig()
		if err != nil {
			return fmt.Errorf("failed to get kubeconfig: %w", err)
		}
		return testregistry.Deploy(context.Background(), cfg, testregistry.DefaultNamespace, testregistry.DefaultName)
	})
	startRegistryPortForward = sync.OnceValues(func() (string, error) {
		if err := deployImageRegistry(); err != nil {
			return "", err
		}
		cfg, err := ctrl.GetConfig()
		if err != nil {
			return "", fmt.Errorf("failed to get kubeconfig: %w", err)
		}
		// Port-forward lives for the duration of the test process;
		// the stop function is not needed because the goroutine is
		// cleaned up on process exit.
		localAddr, _, err := testregistry.PortForward(context.Background(), cfg, testregistry.DefaultNamespace, testregistry.DefaultName)
		if err != nil {
			return "", fmt.Errorf("failed to start port-forward to registry: %w", err)
		}
		return localAddr, nil
	})
)

func RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^OLM is available$`, OLMisAvailable)
	sc.Step(`^(?i)an image registry is available$`, ImageRegistryIsAvailable)

	sc.Step(`^(?i)resource "([^"]+)" UID is remembered$`, RememberResourceUID)
	sc.Step(`^(?i)resource "([^"]+)" keeps its UID$`, ResourceKeepsUID)

	sc.Step(`^(?i)resource "([^"]+)" is installed$`, ResourceAvailable)
	sc.Step(`^(?i)resource "([^"]+)" is available$`, ResourceAvailable)
	sc.Step(`^(?i)resource "([^"]+)" is removed$`, ResourceRemoved)
	sc.Step(`^(?i)resource "([^"]+)" is (?:eventually not found|not found|not installed)$`, ResourceEventuallyNotFound)
	sc.Step(`^(?i)resource "([^"]+)" exists$`, ResourceAvailable)
	sc.Step(`^(?i)resource is applied$`, ResourceIsApplied)
	sc.Step(`^(?i)namespace is applied$`, ResourceIsApplied)
	sc.Step(`^(?i)deployment "([^"]+)" reports as (not ready|ready)$`, MarkDeploymentReadiness)

	sc.Step(`^(?i)resource apply fails with error msg containing "([^"]+)"$`, ResourceApplyFails)
	sc.Step(`^(?i)resource create fails with error msg containing "([^"]+)"$`, ResourceCreateFails)
	sc.Step(`^(?i)resource is applied as user "([^"]+)" in group "([^"]+)"$`, ResourceIsAppliedAsUser)
	sc.Step(`^(?i)resource apply as user "([^"]+)" in group "([^"]+)" fails with error msg containing "([^"]+)"$`, ResourceApplyAsUserFails)
	sc.Step(`^(?i)\w+ apply emits warning:$`, ResourceApplyEmitsWarning)
	sc.Step(`^(?i)\w+ apply does not emit warning$`, ResourceApplyDoesNotEmitWarning)
	sc.Step(`^(?i)resource "([^"]+)" is eventually restored$`, ResourceRestored)
	sc.Step(`^(?i)resource "([^"]+)" matches$`, ResourceMatches)
	sc.Step(`^(?i)rollout restart is performed on "([^"]+)"$`, RolloutRestartIsPerformed)
	sc.Step(`^(?i)annotations are added to "([^"]+)"$`, AnnotationsAreAdded)
	sc.Step(`^(?i)labels are added to "([^"]+)"$`, LabelsAreAdded)
	sc.Step(`^(?i)resource "([^"]+)" has annotations$`, ResourceHasAnnotations)
	sc.Step(`^(?i)resource "([^"]+)" has labels$`, ResourceHasLabels)
	sc.Step(`^(?i)deployment "([^"]+)" pod template has annotation "([^"]+)"$`, DeploymentPodTemplateHasAnnotation)
	sc.Step(`^(?i)deployment "([^"]+)" rollout is complete$`, DeploymentRolloutIsComplete)
	sc.Step(`^(?i)deployment "([^"]+)" has (\d+) replica sets?$`, DeploymentHasReplicaSets)

	sc.Step(`^(?i)ServiceAccount "([^"]*)" with permissions to install extensions is available in "([^"]*)" namespace$`, ServiceAccountWithNeededPermissionsIsAvailableInGivenNamespace)
	sc.Step(`^(?i)namespace "([^"]*)" is available$`, NamespaceIsAvailable)
	sc.Step(`^(?i)ServiceAccount "([^"]*)" with needed permissions is available in test namespace$`, ServiceAccountWithNeededPermissionsIsAvailableInTestNamespace)

	sc.Step(`^(?i)ServiceAccount "([^"]*)" is available in test namespace$`, ServiceAccountIsAvailableInNamespace)
	sc.Step(`^(?i)ServiceAccount "([^"]*)" in test namespace is cluster admin$`, ServiceAccountWithClusterAdminPermissionsIsAvailableInNamespace)
	sc.Step(`^(?i)ServiceAccount "([^"]+)" in test namespace has permissions to fetch "([^"]+)" metrics$`, ServiceAccountWithFetchMetricsPermissions)
	sc.Step(`^(?i)ServiceAccount "([^"]+)" sends request to "([^"]+)" endpoint of "([^"]+)" service$`, SendMetricsRequest)

	sc.Step(`^(?i)catalog "([^"]+)" is deleted$`, ScenarioCatalogIsDeleted)
	sc.Step(`^(?i)catalog "([^"]+)" is updated to version "([^"]+)"$`, ScenarioCatalogIsUpdatedToVersion)
	sc.Step(`^(?i)catalog "([^"]+)" version "([^"]+)" with packages:$`, CatalogVersionWithPackages)
	sc.Step(`^(?i)catalog "([^"]+)" image version "([^"]+)" is also tagged as "([^"]+)"$`, ScenarioCatalogTagImage)
	sc.Step(`^(?i)a catalog "([^"]+)" with packages:$`, CatalogWithPackages)
	sc.Step(`^(?i)catalog "([^"]+)" is labeled with "([^"]+)"$`, CatalogIsLabeledWith)
	sc.Step(`^(?i)ValidatingAdmissionPolicy "([^"]+)" is active$`, ValidatingAdmissionPolicyIsActive)

	sc.Step(`^(?i)namespace "([^"]+)" has labels$`, NamespaceHasLabels)
	sc.Step(`^(?i)namespace "([^"]+)" does not have label "([^"]+)"$`, NamespaceDoesNotHaveLabel)
	sc.Step(`^(?i)namespace "([^"]+)" has no owner references$`, NamespaceHasNoOwnerReferences)

	sc.Step(`^(?i)Prometheus metrics are returned in the response$`, PrometheusMetricsAreReturned)

	sc.Step(`^(?i)min value for (ClusterExtension|ClusterObjectSet|ClusterObjectDeployment) ((?:\.[a-zA-Z]+)+) is set to (\d+)$`, SetCRDFieldMinValue)

	// TLS profile enforcement steps — deployment configuration

	// TLS profile enforcement steps — connection assertions

	// Upgrade-specific steps
	sc.Step(`^(?i)the latest stable OLM release is installed$`, LatestStableOLMReleaseIsInstalled)
	sc.Step(`^(?i)OLM is upgraded$`, OLMIsUpgraded)
	sc.Step(`^(?i)(catalogd|operator-controller) is ready to reconcile resources$`, ComponentIsReadyToReconcile)
	sc.Step(`^(?i)all (ClusterCatalog|ClusterExtension) resources are reconciled$`, allResourcesAreReconciled)
	sc.Step(`^(?i)catalog "([^"]+)" is reconciled$`, ScenarioCatalogIsReconciled)
	sc.Step(`^(?i)catalog "([^"]+)" reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+)$`, ScenarioCatalogReportsCondition)
}

func init() {
	flagSet := pflag.CommandLine
	flagSet.StringVar(&K8sCLI, "k8s.cli", "kubectl", "Path to k8s cli")
	if v, found := os.LookupEnv("KUBECONFIG"); found {
		KubeconfigPath = v
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			panic(fmt.Sprintf("cannot determine user home directory: %v", err))
		}
		flagSet.StringVar(&KubeconfigPath, "kubeconfig", filepath.Join(home, ".kube", "config"), "Paths to a kubeconfig. Only required if out-of-cluster.")
	}
}

// NamespaceForComponent returns the namespace for the named OLM component.
// Falls back to OLMNamespace when the component has no dedicated entry (e.g. upstream single-namespace deployments).
func NamespaceForComponent(component string) string {
	if ns, ok := ComponentNamespaces[component]; ok {
		return ns
	}
	return OLMNamespace
}

func K8sClient(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, K8sCLI, args...)
	logger.V(1).Info("Running", "command", strings.Join(cmd.Args, " "))
	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", KubeconfigPath))

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	output := stdoutBuf.String()
	stderrStr := stderrBuf.String()

	if err != nil {
		logger.V(1).Info("Failed to run", "command", strings.Join(cmd.Args, " "), "stderr", stderrStr, "error", err)
		// Inject stderr into ExitError so stderrOutput() can extract it.
		// cmd.Run() with explicit cmd.Stderr does not populate ExitError.Stderr.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitErr.Stderr = []byte(stderrStr)
		}
	}
	logger.V(1).Info("Output", "command", strings.Join(cmd.Args, " "), "output", output)

	if rec := CommandRecorderFromContext(ctx); rec != nil {
		rec.RecordCommand(strings.Join(cmd.Args, " "), output, stderrStr, elapsed)
	}
	return output, err
}

func K8sCLIWithInput(ctx context.Context, yaml string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, K8sCLI, args...)
	cmd.Stdin = bytes.NewBufferString(yaml)
	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", KubeconfigPath))

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	output := stdoutBuf.String()
	stderrStr := stderrBuf.String()

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitErr.Stderr = []byte(stderrStr)
		}
	}

	if rec := CommandRecorderFromContext(ctx); rec != nil {
		rec.RecordCommandWithInput(strings.Join(cmd.Args, " "), yaml, output, stderrStr, elapsed)
	}
	return output, stderrStr, err
}

// projectRootDir finds the project root by walking up from the source file until go.mod is found.
func projectRootDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find project root (no go.mod found)")
		}
		dir = parent
	}
}

// registryHosts returns the local and in-cluster registry addresses.
// The local address is obtained by port-forwarding to the in-cluster registry,
// which works regardless of the cluster's network topology.
func registryHosts() (string, string, error) {
	local, err := startRegistryPortForward()
	if err != nil {
		return "", "", fmt.Errorf("failed to get local registry address: %w", err)
	}
	cluster := os.Getenv("CLUSTER_REGISTRY_HOST")
	if cluster == "" {
		cluster = "docker-registry.operator-controller-e2e.svc.cluster.local:5000"
	}
	return local, cluster, nil
}

// ImageRegistryIsAvailable ensures the in-cluster image registry is deployed and ready.
// This is needed for scenarios that build and push per-scenario catalog images.
func ImageRegistryIsAvailable() error {
	return deployImageRegistry()
}

// OLMisAvailable waits for the OLM operator-controller deployment to become available. Polls with timeout.
func OLMisAvailable(ctx context.Context) error {
	require.Eventually(godog.T(ctx), func() bool {
		v, err := K8sClient(ctx, "get", "deployment", "-n", OLMNamespace, OperatorControllerDeploymentName, "-o", "json")
		if err != nil {
			return false
		}
		var deployment appsv1.Deployment
		if err := json.Unmarshal([]byte(v), &deployment); err != nil || !GenerationObserved(deployment.Generation, deployment.Status.ObservedGeneration) {
			return false
		}
		condition := deploymentutil.GetDeploymentCondition(deployment.Status, appsv1.DeploymentAvailable)
		return condition != nil && condition.Status == corev1.ConditionTrue
	}, Timeout, Tick)

	if rec := CommandRecorderFromContext(ctx); rec != nil {
		out, _ := K8sClient(ctx, "get", "deployments", "-n", OLMNamespace, "-l", "app.kubernetes.io/part-of=olm")
		rec.RecordCustom(fmt.Sprintf("kubectl get deployments -n %s", OLMNamespace), out, "")
	}
	return nil
}

func ToUnstructured(yamlContent string) (*unstructured.Unstructured, error) {
	var u map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &u); err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: u}, nil
}

func injectTestAnnotations(obj *unstructured.Unstructured, sc *World) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations["e2e.olm.operatorframework.io/feature"] = sc.FeatureName
	annotations["e2e.olm.operatorframework.io/scenario"] = sc.ScenarioName
	obj.SetAnnotations(annotations)
}

func SubstituteScenarioVars(content string, sc *World) string {
	vars := map[string]string{
		"TEST_NAMESPACE": sc.Namespace,
		"NAME":           sc.ClusterExtensionName,
		"COS_NAME":       sc.ClusterObjectSetName,
		"SCENARIO_ID":    sc.ID,
		"OLM_NAMESPACE":  OLMNamespace,
	}
	for orig, param := range sc.CatalogPackageNames {
		vars[fmt.Sprintf("PACKAGE:%s", orig)] = param
	}
	for name, catalogName := range sc.Catalogs {
		vars[fmt.Sprintf("CATALOG:%s", name)] = catalogName
	}
	return templateContent(content, vars)
}

// ResourceApplyFails waits for kubectl apply of the provided YAML to fail with the expected error message. Polls with timeout.
func ResourceApplyFails(ctx context.Context, errMsg string, yamlTemplate *godog.DocString) error {
	sc := Scenario(ctx)
	yamlContent := SubstituteScenarioVars(yamlTemplate.Content, sc)
	_, err := ToUnstructured(yamlContent)
	if err != nil {
		return fmt.Errorf("failed to parse resource yaml: %v", err)
	}
	WaitFor(ctx, func() bool {
		_, stdErr, err := K8sCLIWithInput(ctx, yamlContent, "apply", "-f", "-")
		if err == nil {
			return false
		}
		if !strings.Contains(stdErr, errMsg) {
			return false
		}
		return true
	})
	return nil
}

// ResourceCreateFails checks a create request, so an existing object cannot be updated by apply.
func ResourceCreateFails(ctx context.Context, errMsg string, yamlTemplate *godog.DocString) error {
	sc := Scenario(ctx)
	yamlContent := SubstituteScenarioVars(yamlTemplate.Content, sc)
	if _, err := ToUnstructured(yamlContent); err != nil {
		return fmt.Errorf("failed to parse resource yaml: %w", err)
	}
	_, stderr, err := K8sCLIWithInput(ctx, yamlContent, "create", "-f", "-")
	if err == nil {
		return fmt.Errorf("expected create error containing %q, but creation succeeded", errMsg)
	}
	if !strings.Contains(stderr, errMsg) {
		return fmt.Errorf("expected create error containing %q, got: %s: %w", errMsg, stderr, err)
	}
	return nil
}

// ResourceIsAppliedAsUser applies the provided YAML using kubectl impersonation (--as/--as-group).
func ResourceIsAppliedAsUser(ctx context.Context, user, group string, yamlTemplate *godog.DocString) error {
	sc := Scenario(ctx)
	yamlContent := SubstituteScenarioVars(yamlTemplate.Content, sc)
	res, err := ToUnstructured(yamlContent)
	if err != nil {
		return fmt.Errorf("failed to parse resource yaml: %v", err)
	}
	injectTestAnnotations(res, sc)
	annotatedYAML, err := yaml.Marshal(res.Object)
	if err != nil {
		return fmt.Errorf("failed to marshal resource yaml: %w", err)
	}
	out, stdErr, err := K8sCLIWithInput(ctx, string(annotatedYAML), "apply", "-f", "-", "--as="+user, "--as-group="+group)
	if err != nil {
		return fmt.Errorf("failed to apply resource as %s/%s: %v; stderr: %s", user, group, out, stdErr)
	}
	sc.LastApplyStderr = stdErr
	if res.GetKind() == "ClusterExtension" {
		sc.AddedResources = append(sc.AddedResources, Resource{Name: res.GetName(), Kind: "clusterextension"})
	} else {
		namespace := res.GetNamespace()
		if namespace == "" {
			namespace = sc.Namespace
		}
		sc.AddedResources = append(sc.AddedResources, Resource{
			Name:      res.GetName(),
			Kind:      strings.ToLower(res.GetKind()),
			Namespace: namespace,
		})
	}
	return nil
}

// ResourceApplyAsUserFails polls kubectl apply --dry-run=server with impersonation until the admission controller
// denies the request with the expected error message. Server-side dry-run sends each request through the full
// admission chain without persisting the resource, which allows polling until eventually-consistent admission
// policies (e.g. ValidatingAdmissionPolicy) become active. Once the dry-run confirms denial, a real apply
// (without dry-run) is issued to verify the actual request is also denied.
func ResourceApplyAsUserFails(ctx context.Context, user, group, errMsg string, yamlTemplate *godog.DocString) error {
	sc := Scenario(ctx)
	yamlContent := SubstituteScenarioVars(yamlTemplate.Content, sc)
	res, err := ToUnstructured(yamlContent)
	if err != nil {
		return fmt.Errorf("failed to parse resource yaml: %v", err)
	}
	// dry-run=server: ValidatingAdmissionPolicy enforcement is eventually consistent — the API server's
	// informer cache may not have the binding yet. Dry-run avoids persisting the resource on the first
	// attempt, so we can safely poll until the admission chain starts denying.
	WaitFor(ctx, func() bool {
		_, stdErr, err := K8sCLIWithInput(ctx, yamlContent, "apply", "--dry-run=server", "-f", "-", "--as="+user, "--as-group="+group)
		if err == nil {
			return false
		}
		return strings.Contains(stdErr, errMsg)
	})
	// The dry-run above may have been served by a kube-apiserver replica that already enforces the
	// binding while the real apply is load-balanced to a different replica that has not caught up yet —
	// VAP binding enforcement propagates per-replica and is eventually consistent, so a single dry-run
	// denial does not guarantee the next request is denied. Poll the real apply until it is denied with
	// the expected message; if a request slips through the propagation window, the object is persisted,
	// so delete it and retry. This converges without leaking the resource or reporting a false
	// "it succeeded".
	var lastErr error
	// pendingCleanup is true once an apply has been admitted and the persisted object has not yet been
	// confirmed removed. We must not report success while a leaked object remains — a following
	// "is not found" step would time out, and ScenarioCleanup skips tracked-resource deletion once the
	// scenario has failed, so the object would also leak into the next scenario.
	pendingCleanup := false
	deadline := time.Now().Add(Timeout)
	for {
		_, stdErr, applyErr := K8sCLIWithInput(ctx, yamlContent, "apply", "-f", "-", "--as="+user, "--as-group="+group)
		switch {
		case applyErr == nil:
			// Admitted before enforcement propagated to this replica. Track for cleanup and remove it below.
			if !pendingCleanup {
				sc.AddedResources = append(sc.AddedResources, Resource{Name: res.GetName(), Kind: strings.ToLower(res.GetKind())})
			}
			pendingCleanup = true
			lastErr = fmt.Errorf("apply was admitted before enforcement propagated")
		case strings.Contains(stdErr, errMsg):
			// Denied with the expected message. Succeed only once any earlier-admitted object is gone.
			if !pendingCleanup {
				return nil
			}
		default:
			lastErr = fmt.Errorf("expected apply error containing %q, got: %s", errMsg, stdErr)
		}
		if pendingCleanup {
			// A blocking delete (default --wait) returns only once the object is actually removed, so a
			// nil error confirms removal. Keep pendingCleanup set on failure so a later denial is not accepted.
			// Bound the wait by the remaining step budget (at least one tick) so a finalizer-blocked delete —
			// e.g. a ClusterExtension waiting on its controller — cannot push the step past deadline.
			remaining := time.Until(deadline)
			if remaining < Tick {
				remaining = Tick
			}
			delArgs := []string{"delete", res.GetKind(), res.GetName(), "--ignore-not-found", "--timeout=" + remaining.String()}
			if ns := res.GetNamespace(); ns != "" {
				delArgs = append(delArgs, "-n", ns)
			}
			if _, delErr := K8sClient(ctx, delArgs...); delErr != nil {
				lastErr = fmt.Errorf("failed to remove admitted resource: %w", delErr)
			} else {
				pendingCleanup = false
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("expected apply to fail with %q within %s: %w", errMsg, Timeout, lastErr)
		}
		time.Sleep(Tick)
	}
}

// ResourceApplyEmitsWarning asserts the last kubectl apply produced stderr output containing the expected warning.
func ResourceApplyEmitsWarning(ctx context.Context, expected *godog.DocString) error {
	sc := Scenario(ctx)
	expectedText := strings.TrimSpace(expected.Content)
	if !strings.Contains(sc.LastApplyStderr, expectedText) {
		return fmt.Errorf("expected apply warning %q, got stderr: %q", expectedText, sc.LastApplyStderr)
	}
	return nil
}

func ResourceApplyDoesNotEmitWarning(ctx context.Context) error {
	sc := Scenario(ctx)
	if strings.Contains(sc.LastApplyStderr, "Warning:") {
		return fmt.Errorf("expected no warnings, got stderr: %q", sc.LastApplyStderr)
	}
	return nil
}

// ResourceIsApplied applies the provided YAML resource to the cluster and in case of ClusterExtension or ClusterObjectSet it captures
// its name in the test context so that it can be referred to in later steps with ${NAME} or ${COS_NAME}, respectively
func ResourceIsApplied(ctx context.Context, yamlTemplate *godog.DocString) error {
	sc := Scenario(ctx)
	yamlContent := SubstituteScenarioVars(yamlTemplate.Content, sc)
	res, err := ToUnstructured(yamlContent)
	if err != nil {
		return fmt.Errorf("failed to parse resource yaml: %v", err)
	}
	injectTestAnnotations(res, sc)
	annotatedYAML, err := yaml.Marshal(res.Object)
	if err != nil {
		return fmt.Errorf("failed to marshal resource yaml: %w", err)
	}
	out, stdErr, err := K8sCLIWithInput(ctx, string(annotatedYAML), "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("failed to apply resource %v; err: %w; stderr: %s", out, err, stdErr)
	}
	sc.LastApplyStderr = stdErr
	if res.GetKind() == "ClusterExtension" {
		sc.AddedResources = append(sc.AddedResources, Resource{Name: res.GetName(), Kind: "clusterextension"})
	} else if res.GetKind() == "ClusterObjectSet" {
		sc.ClusterObjectSetName = res.GetName() // used for ${COS_NAME} variable substitution in YAML templates
		sc.AddedResources = append(sc.AddedResources, Resource{Name: res.GetName(), Kind: "clusterobjectset"})
	} else {
		namespace := res.GetNamespace()
		if namespace == "" {
			namespace = sc.Namespace
		}
		sc.AddedResources = append(sc.AddedResources, Resource{
			Name:      res.GetName(),
			Kind:      strings.ToLower(res.GetKind()),
			Namespace: namespace,
		})
	}
	return nil
}

func WaitFor(ctx context.Context, conditionFn func() bool) {
	require.Eventually(godog.T(ctx), conditionFn, Timeout, Tick)
}

type MsgMatchFn func(string) bool

func AlwaysMatch(_ string) bool { return true }

func MessageComparison(ctx context.Context, msg *godog.DocString) MsgMatchFn {
	msgCmp := AlwaysMatch
	if msg != nil {
		expectedMsg := SubstituteScenarioVars(strings.Join(strings.Fields(msg.Content), " "), Scenario(ctx))
		msgCmp = func(actual string) bool {
			return actual == expectedMsg
		}
	}
	return msgCmp
}

func MessageFragmentComparison(ctx context.Context, msgFragment *godog.DocString) MsgMatchFn {
	if msgFragment == nil {
		return AlwaysMatch
	}
	expectedFragment := SubstituteScenarioVars(strings.Join(strings.Fields(msgFragment.Content), " "), Scenario(ctx))
	return func(actualMsg string) bool {
		return strings.Contains(strings.Join(strings.Fields(actualMsg), " "), expectedFragment)
	}
}

// WaitForCondition requires the asserted condition to reflect metadata.generation,
// in addition to matching the requested status, reason, and message.
func WaitForCondition(ctx context.Context, resourceType, resourceName, conditionType string, conditionStatus metav1.ConditionStatus, conditionReason *string, msgCmp MsgMatchFn) error {
	require.Eventually(godog.T(ctx), func() bool {
		resource, err := ReadResourceConditions(ctx, resourceType, resourceName)
		if err != nil {
			return false
		}
		condition := CurrentCondition(resource.Metadata.Generation, resource.Status.Conditions, conditionType)
		return ConditionMatches(condition, conditionStatus, conditionReason, msgCmp)
	}, Timeout, Tick)
	return nil
}

// consistently invokes check every tick until the expected state has held for the full
// window, then returns success. Each call is one observation of the live cluster state:
// check returns a non-nil error to report a transient failure whose true state is unknown
// (e.g. a failed API read), which is tolerated only within maxTransientGap; it uses
// require/assert for hard failures that must fail the test immediately (e.g. an observed
// value that deviates from what is expected). A nil return is a successful observation.
//
// The window is measured from the first successful observation, not from the step start,
// so a transient read outage at the start of the window delays it rather than failing it
// (maxTransientGap can exceed a short window, in which case the start-of-step behaviour
// would otherwise fail the step without ever reading the resource). consistently still
// fails if no observation ever succeeds (it cannot pass vacuously when the resource is
// never successfully read) or if reads stay unavailable for longer than maxTransientGap.
func consistently(ctx context.Context, duration time.Duration, check func(context.Context) error) error {
	t := godog.T(ctx)
	const maxTransientGap = 10 * time.Second
	var windowStart time.Time  // set on the first successful observation; the window is measured from here
	lastObserved := time.Now() // last successful observation (or start), to bound transient read outages
	observed := false
	// Allow the window itself plus one transient-gap of slack, so an outage at the start of
	// the window postpones it instead of failing. A transient check error is tolerated only
	// within maxTransientGap of the last successful observation; a hard failure inside check
	// fails the test immediately via require.
	err := wait.PollUntilContextTimeout(ctx, Tick, duration+maxTransientGap, true, func(ctx context.Context) (bool, error) {
		if cerr := check(ctx); cerr != nil {
			require.LessOrEqualf(t, time.Since(lastObserved), maxTransientGap,
				"consistency check could not observe the expected state for over %s: %v", maxTransientGap, cerr)
			return false, nil
		}
		now := time.Now()
		observed = true
		lastObserved = now
		if windowStart.IsZero() {
			windowStart = now
		}
		// Stop once the expected state has held for the full window since the first
		// successful observation. A deviation during the window fails hard inside check.
		return now.Sub(windowStart) >= duration, nil
	})
	// Reaching the overall deadline without the window completing is only acceptable if we at
	// least observed the state (checked below); anything else is a real error.
	if err != nil && !wait.Interrupted(err) {
		return err
	}
	require.Truef(t, observed,
		"consistency check never observed the expected state during the window")
	return nil
}

// conditionCheck returns a consistently check that verifies a single condition's status
// and reason for the current generation on the named resource. A failed API read is returned as a transient error
// (the condition's real state is unknown while the read is failing). A successful read
// that does not contain the condition, or shows a different status or reason, fails
// immediately: a condition that disappears or changes is not being reported consistently.
func conditionCheck(resourceType, resourceName, conditionType string, conditionStatus metav1.ConditionStatus, conditionReason string) func(context.Context) error {
	return func(ctx context.Context) error {
		t := godog.T(ctx)
		resource, err := ReadResourceConditions(ctx, resourceType, resourceName)
		if err != nil {
			return err
		}
		condition := CurrentCondition(resource.Metadata.Generation, resource.Status.Conditions, conditionType)
		require.NotNilf(t, condition,
			"%s %s condition disappeared or has not observed generation %d during the consistency window", resourceType, conditionType, resource.Metadata.Generation)
		require.Equalf(t, conditionStatus, condition.Status,
			"%s %s condition status changed during the consistency window", resourceType, conditionType)
		require.Equalf(t, conditionReason, condition.Reason,
			"%s %s condition reason changed during the consistency window", resourceType, conditionType)
		return nil
	}
}

// ConsistentlyCondition fails the test if, at any point during duration, the named
// resource's condition deviates from the expected type/status/reason or disappears.
// Transient API read failures are tolerated only briefly; see consistently and
// conditionCheck for the exact semantics.
func ConsistentlyCondition(ctx context.Context, resourceType, resourceName, conditionType string, conditionStatus metav1.ConditionStatus, conditionReason string, duration time.Duration) error {
	return consistently(ctx, duration, conditionCheck(resourceType, resourceName, conditionType, conditionStatus, conditionReason))
}

// ResourceAvailable waits for the specified resource (kind/name format) to exist in the test namespace. Polls with timeout.
func ResourceAvailable(ctx context.Context, resource string) error {
	sc := Scenario(ctx)
	resource = SubstituteScenarioVars(resource, sc)
	kind, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <kind>/<name>", resource)
	}
	WaitFor(ctx, func() bool {
		_, err := K8sClient(ctx, "get", kind, name, "-n", sc.Namespace)
		return err == nil
	})
	return nil
}

// ResourceRemoved saves the resource state for potential restore checks, then deletes it from the test namespace.
func ResourceRemoved(ctx context.Context, resource string) error {
	sc := Scenario(ctx)
	resource = SubstituteScenarioVars(resource, sc)
	kind, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <kind>/<name>", resource)
	}
	yaml, err := K8sClient(ctx, "get", kind, name, "-n", sc.Namespace, "-o", "yaml")
	if err != nil {
		return err
	}
	obj, err := ToUnstructured(yaml)
	if err != nil {
		return err
	}
	sc.RemovedResources = append(sc.RemovedResources, *obj)
	_, err = K8sClient(ctx, "delete", kind, name, "-n", sc.Namespace)
	return err
}

// ResourceEventuallyNotFound waits for the specified resource to be fully deleted from the test namespace. Polls with timeout.
func ResourceEventuallyNotFound(ctx context.Context, resource string) error {
	sc := Scenario(ctx)
	resource = SubstituteScenarioVars(resource, sc)
	kind, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <kind>/<name>", resource)
	}

	WaitFor(ctx, func() bool {
		obj, err := K8sClient(ctx, "get", kind, name, "-n", sc.Namespace, "--ignore-not-found", "-o", "yaml")
		return err == nil && strings.TrimSpace(obj) == ""
	})
	return nil
}

// ResourceMatches waits for the specified resource to match the expected content using JSON merge patch comparison. Polls with timeout.
func ResourceMatches(ctx context.Context, resource string, requiredContentTemplate *godog.DocString) error {
	sc := Scenario(ctx)
	resource = SubstituteScenarioVars(resource, sc)
	kind, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <kind>/<name>", resource)
	}
	requiredContent, err := ToUnstructured(SubstituteScenarioVars(requiredContentTemplate.Content, sc))
	if err != nil {
		return fmt.Errorf("failed to parse required resource yaml: %v", err)
	}
	WaitFor(ctx, func() bool {
		objJson, err := K8sClient(ctx, "get", kind, name, "-n", sc.Namespace, "-o", "json")
		if err != nil {
			return false
		}
		obj, err := ToUnstructured(objJson)
		if err != nil {
			return false
		}
		patch, err := json.Marshal(requiredContent.Object)
		if err != nil {
			return false
		}
		updJson, err := jsonpatch.MergePatch([]byte(objJson), patch)
		if err != nil {
			return false
		}
		upd, err := ToUnstructured(string(updJson))
		if err != nil {
			return false
		}

		return len(cmp.Diff(upd.Object, obj.Object)) == 0
	})
	return nil
}

// ResourceRestored waits for a previously removed resource to be re-created with matching spec or data. Polls with timeout.
func ResourceRestored(ctx context.Context, resource string) error {
	sc := Scenario(ctx)
	resource = SubstituteScenarioVars(resource, sc)
	kind, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <kind>/<name>", resource)
	}
	WaitFor(ctx, func() bool {
		yaml, err := K8sClient(ctx, "get", kind, name, "-n", sc.Namespace, "-o", "yaml")
		if err != nil {
			return false
		}
		obj, err := ToUnstructured(yaml)
		if err != nil {
			return false
		}
		ct := obj.GetCreationTimestamp()

		for i, removed := range sc.RemovedResources {
			rct := removed.GetCreationTimestamp()
			if removed.GetName() == obj.GetName() && removed.GetKind() == obj.GetKind() && rct.Before(&ct) {
				switch kind {
				case "configmap":
					if !reflect.DeepEqual(removed.Object["data"], obj.Object["data"]) {
						return false
					}
				default:
					if !reflect.DeepEqual(removed.Object["spec"], obj.Object["spec"]) {
						return false
					}
				}
				sc.RemovedResources = append(sc.RemovedResources[:i], sc.RemovedResources[i+1:]...)
				return true
			}
		}
		return false
	})
	return nil
}

func applyServiceAccount(ctx context.Context, serviceAccount string, keyValue ...string) error {
	sc := Scenario(ctx)
	vars := extendMap(map[string]string{
		"TEST_NAMESPACE":       sc.Namespace,
		"SERVICE_ACCOUNT_NAME": serviceAccount,
		"SERVICEACCOUNT_NAME":  serviceAccount,
	}, keyValue...)

	_, thisFile, _, _ := runtime.Caller(0)
	yaml, err := templateYaml(filepath.Join(filepath.Dir(thisFile), "testdata", "serviceaccount-template.yaml"), vars)
	if err != nil {
		return fmt.Errorf("failed to template ServiceAccount yaml: %v", err)
	}

	// Apply the ServiceAccount configuration
	_, stdErr, err := K8sCLIWithInput(ctx, yaml, "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("failed to apply ServiceAccount configuration: %v: %s", err, stdErr)
	}

	return nil
}

func applyPermissionsToServiceAccount(ctx context.Context, serviceAccount, rbacTemplate string, keyValue ...string) error {
	sc := Scenario(ctx)
	if err := applyServiceAccount(ctx, serviceAccount); err != nil {
		return err
	}
	vars := extendMap(map[string]string{
		"TEST_NAMESPACE":         sc.Namespace,
		"SERVICE_ACCOUNT_NAME":   serviceAccount,
		"SERVICEACCOUNT_NAME":    serviceAccount,
		"CLUSTER_EXTENSION_NAME": sc.ClusterExtensionName,
		"CLUSTEREXTENSION_NAME":  sc.ClusterExtensionName,
	}, keyValue...)

	_, thisFile, _, _ := runtime.Caller(0)
	rbacYaml, err := templateYaml(filepath.Join(filepath.Dir(thisFile), "testdata", rbacTemplate), vars)
	if err != nil {
		return fmt.Errorf("failed to template RBAC yaml: %v", err)
	}

	// Apply the RBAC configuration
	_, rbacStdErr, err := K8sCLIWithInput(ctx, rbacYaml, "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("failed to apply RBAC configuration: %v: %s", err, rbacStdErr)
	}

	// Track cluster-scoped RBAC resources for cleanup
	for _, doc := range strings.Split(rbacYaml, "---") {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}
		res, err := ToUnstructured(doc)
		if err != nil {
			continue
		}
		kind := res.GetKind()
		if kind == "ClusterRole" || kind == "ClusterRoleBinding" {
			sc.AddedResources = append(sc.AddedResources, Resource{Name: res.GetName(), Kind: strings.ToLower(kind)})
		}
	}

	return nil
}

// NamespaceIsAvailable ensures the given namespace exists by applying a namespace template.
func NamespaceIsAvailable(ctx context.Context, ns string) error {
	sc := Scenario(ctx)
	ns = SubstituteScenarioVars(ns, sc)
	_, thisFile, _, _ := runtime.Caller(0)
	yaml, err := templateYaml(filepath.Join(filepath.Dir(thisFile), "testdata", "namespace-template.yaml"), map[string]string{
		"NAMESPACE": ns,
	})
	if err != nil {
		return fmt.Errorf("failed to template namespace yaml: %v", err)
	}
	if _, _, err := K8sCLIWithInput(ctx, yaml, "apply", "-f", "-"); err != nil {
		return fmt.Errorf("failed to apply namespace %s: %w", ns, err)
	}
	return nil
}

// ServiceAccountIsAvailableInNamespace creates a ServiceAccount in the test namespace without RBAC permissions.
func ServiceAccountIsAvailableInNamespace(ctx context.Context, serviceAccount string) error {
	return applyServiceAccount(ctx, serviceAccount)
}

// ServiceAccountWithNeededPermissionsIsAvailableInTestNamespace creates a ServiceAccount and applies standard RBAC permissions.
// Direct object-controller scenarios select Boxcutter RBAC by tag; integrated
// scenarios use the BoxcutterRuntime gate, independently of feature filenames.
func ServiceAccountWithNeededPermissionsIsAvailableInTestNamespace(ctx context.Context, serviceAccount string) error {
	kernel := "helm"
	if FeatureGates[features.BoxcutterRuntime] || Scenario(ctx).Tags.Has("@"+string(ObjectControllerFeature)) {
		kernel = "boxcutter"
	}
	rbacTemplate := fmt.Sprintf("%s-%s-rbac-template.yaml", serviceAccount, kernel)
	return applyPermissionsToServiceAccount(ctx, serviceAccount, rbacTemplate)
}

// ServiceAccountWithNeededPermissionsIsAvailableInGivenNamespace creates a ServiceAccount and enables creation of any cluster extension on behalf of this account.
func ServiceAccountWithNeededPermissionsIsAvailableInGivenNamespace(ctx context.Context, serviceAccount string, ns string) error {
	sc := Scenario(ctx)
	ns = SubstituteScenarioVars(ns, sc)
	sc.AddedResources = append(sc.AddedResources, Resource{Name: ns, Kind: "namespace"})
	if err := applyServiceAccount(ctx, serviceAccount, "TEST_NAMESPACE", ns); err != nil {
		return err
	}
	return applyPermissionsToServiceAccount(ctx, serviceAccount, "rbac-template-for-all-extensions.yaml", "TEST_NAMESPACE", ns)
}

// ServiceAccountWithClusterAdminPermissionsIsAvailableInNamespace creates a ServiceAccount and applies cluster-admin RBAC.
func ServiceAccountWithClusterAdminPermissionsIsAvailableInNamespace(ctx context.Context, serviceAccount string) error {
	return applyPermissionsToServiceAccount(ctx, serviceAccount, "cluster-admin-rbac-template.yaml")
}

// ServiceAccountWithFetchMetricsPermissions creates a ServiceAccount and applies metrics-reader RBAC for the specified controller.
func ServiceAccountWithFetchMetricsPermissions(ctx context.Context, serviceAccount string, controllerName string) error {
	return applyPermissionsToServiceAccount(ctx, serviceAccount, "metrics-reader-rbac-template.yaml", "CONTROLLER_NAME", controllerName)
}

func httpGet(url string, token string) (*http.Response, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // we don't care about the certificate
	}
	client := &http.Client{Transport: tr}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// SendMetricsRequest sets up port-forwarding to the controller's service pods and waits for the metrics endpoint
// to return a successful response. Stores the response body per pod in the scenario context. Polls with timeout.
func SendMetricsRequest(ctx context.Context, serviceAccount string, endpoint string, controllerName string) error {
	sc := Scenario(ctx)
	svc, err := GetMetricsService(controllerName)
	if err != nil {
		return err
	}
	mPort, err := MetricsPort(svc)
	if err != nil {
		return err
	}

	podNameCmd := []string{"get", "pod", "-n", svc.Namespace, "-o", "jsonpath={.items}"}
	for k, v := range svc.Spec.Selector {
		podNameCmd = append(podNameCmd, fmt.Sprintf("--selector=%s=%s", k, v))
	}
	v, err := K8sClient(ctx, podNameCmd...)
	if err != nil {
		return err
	}

	var pods []corev1.Pod
	if err := json.Unmarshal([]byte(v), &pods); err != nil {
		return err
	}
	token, err := K8sClient(ctx, "create", "token", serviceAccount, "-n", sc.Namespace)
	if err != nil {
		return err
	}

	sc.MetricsResponse = make(map[string]string)
	for _, p := range pods {
		if err := func() error {
			addr, cleanup, err := PortForward(ctx, p.Namespace, fmt.Sprintf("pod/%s", p.Name), mPort)
			if err != nil {
				return err
			}
			defer cleanup()
			WaitFor(ctx, func() bool {
				resp, err := httpGet(fmt.Sprintf("https://%s%s", addr, endpoint), token)
				if err != nil {
					return false
				}
				defer resp.Body.Close()

				if resp.StatusCode == http.StatusOK {
					b, err := io.ReadAll(resp.Body)
					if err != nil {
						return false
					}
					sc.MetricsResponse[p.Name] = string(b)
					return true
				}
				b, err := io.ReadAll(resp.Body)
				if err != nil {
					return false
				}
				logger.V(1).Info("failed to get metrics", "pod", p.Name, "response", string(b))
				return false
			})
			return nil
		}(); err != nil {
			return err
		}
	}

	return nil
}

// ScenarioCatalogIsDeleted deletes a named per-scenario ClusterCatalog and waits for it to be removed.
func ScenarioCatalogIsDeleted(ctx context.Context, catalogUserName string) error {
	sc := Scenario(ctx)
	catalogName, ok := sc.Catalogs[catalogUserName]
	if !ok {
		return fmt.Errorf("no catalog %q has been created for this scenario", catalogUserName)
	}
	_, err := K8sClient(ctx, "delete", "clustercatalog", catalogName, "--ignore-not-found=true", "--wait=true")
	if err != nil {
		return fmt.Errorf("failed to delete catalog %q: %v", catalogUserName, err)
	}
	return nil
}

func CatalogIsLabeledWith(ctx context.Context, catalogUserName, label string) error {
	sc := Scenario(ctx)
	catalogName, ok := sc.Catalogs[catalogUserName]
	if !ok {
		return fmt.Errorf("no catalog %q has been created for this scenario", catalogUserName)
	}
	_, err := K8sClient(ctx, "label", "clustercatalog", catalogName, label, "--overwrite")
	if err != nil {
		return fmt.Errorf("failed to label catalog %q with %q: %v", catalogUserName, label, err)
	}
	return nil
}

// ValidatingAdmissionPolicyIsActive waits for the named ValidatingAdmissionPolicy to be compiled
// and active by checking that status.observedGeneration matches metadata.generation.
// The scenario fails on timeout if the policy never becomes active.
func ValidatingAdmissionPolicyIsActive(ctx context.Context, name string) error {
	sc := Scenario(ctx)
	name = SubstituteScenarioVars(name, sc)
	WaitFor(ctx, func() bool {
		out, err := K8sClient(ctx, "get", "validatingadmissionpolicy", name, "-o",
			"jsonpath={.metadata.generation},{.status.observedGeneration}")
		if err != nil {
			return false
		}
		parts := strings.Split(out, ",")
		return len(parts) == 2 && parts[0] != "" && parts[1] != "" && parts[0] == parts[1]
	})
	return nil
}

// CatalogVersionWithPackages builds an additional version of a per-scenario catalog image.
// It pushes a new catalog image with the specified tag but does NOT update the ClusterCatalog.
// Use ScenarioCatalogIsUpdatedToVersion to patch the ClusterCatalog to use the new version.
func CatalogVersionWithPackages(ctx context.Context, catalogUserName, version string, table *godog.Table) error {
	sc := Scenario(ctx)

	pkgOpts, err := parseCatalogTable(table)
	if err != nil {
		return err
	}

	cat := catalog.NewCatalog(catalogUserName, sc.ID, pkgOpts...)
	localRegistry, clusterRegistry, err := registryHosts()
	if err != nil {
		return err
	}

	result, err := cat.Build(ctx, version, localRegistry, clusterRegistry)
	if err != nil {
		return fmt.Errorf("failed to build catalog version %s: %w", version, err)
	}

	// Accumulate package names (bundles may differ between versions)
	for orig, param := range result.PackageNames {
		sc.CatalogPackageNames[orig] = param
	}

	return nil
}

// ScenarioCatalogIsUpdatedToVersion patches a named per-scenario ClusterCatalog's image ref to a new version tag.
func ScenarioCatalogIsUpdatedToVersion(ctx context.Context, catalogUserName, version string) error {
	sc := Scenario(ctx)
	catalogName, ok := sc.Catalogs[catalogUserName]
	if !ok {
		return fmt.Errorf("no catalog %q has been created for this scenario", catalogUserName)
	}
	ref, err := K8sClient(ctx, "get", "clustercatalog", catalogName, "-o", "jsonpath={.spec.source.image.ref}")
	if err != nil {
		return err
	}
	i := strings.LastIndexByte(ref, ':')
	if i == -1 {
		return fmt.Errorf("failed to find tag in image reference %s", ref)
	}
	base := ref[:i]
	patch := map[string]any{
		"spec": map[string]any{
			"source": map[string]any{
				"image": map[string]any{
					"ref": fmt.Sprintf("%s:%s", base, version),
				},
			},
		},
	}
	pb, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = K8sClient(ctx, "patch", "clustercatalog", catalogName, "--type", "merge", "-p", string(pb))
	return err
}

// ScenarioCatalogTagImage tags an existing per-scenario catalog image with a new tag.
func ScenarioCatalogTagImage(ctx context.Context, catalogUserName, oldTag, newTag string) error {
	localRegistry, _, err := registryHosts()
	if err != nil {
		return err
	}
	sc := Scenario(ctx)
	imageRef := fmt.Sprintf("%s/e2e/%s-catalog-%s:%s", localRegistry, catalogUserName, sc.ID, oldTag)
	return crane.Tag(imageRef, newTag, crane.Insecure)
}

// parseCatalogTable parses a Gherkin data table into catalog.PackageOption slices.
// Table columns: package | version | channel | replaces | contents
func parseCatalogTable(table *godog.Table) ([]catalog.PackageOption, error) {
	type bundleKey struct {
		pkg     string
		version string
	}
	type bundleEntry struct {
		opts []catalog.BundleOption
	}
	type channelEntry struct {
		version  string
		replaces string
	}

	bundleDefs := make(map[bundleKey]*bundleEntry)
	var bundleOrder []bundleKey
	packageChannels := make(map[string]map[string][]channelEntry)
	var packageOrder []string
	packageSeen := make(map[string]bool)

	for _, row := range table.Rows[1:] { // skip header
		pkg := row.Cells[0].Value
		version := row.Cells[1].Value
		channel := row.Cells[2].Value
		replaces := row.Cells[3].Value
		contents := row.Cells[4].Value

		if !packageSeen[pkg] {
			packageOrder = append(packageOrder, pkg)
			packageSeen[pkg] = true
		}

		bk := bundleKey{pkg: pkg, version: version}
		if _, exists := bundleDefs[bk]; exists {
			if strings.TrimSpace(contents) != "" {
				return nil, fmt.Errorf("duplicate bundle %s/%s: contents must be empty for repeated versions", pkg, version)
			}
		} else {
			opts, err := parseContents(contents)
			if err != nil {
				return nil, fmt.Errorf("bundle %s/%s: %w", pkg, version, err)
			}
			bundleDefs[bk] = &bundleEntry{opts: opts}
			bundleOrder = append(bundleOrder, bk)
		}

		if packageChannels[pkg] == nil {
			packageChannels[pkg] = make(map[string][]channelEntry)
		}
		packageChannels[pkg][channel] = append(packageChannels[pkg][channel], channelEntry{
			version:  version,
			replaces: replaces,
		})
	}

	var pkgOpts []catalog.PackageOption
	for _, pkgName := range packageOrder {
		var opts []catalog.PackageOption

		for _, bk := range bundleOrder {
			if bk.pkg == pkgName {
				opts = append(opts, catalog.Bundle(bk.version, bundleDefs[bk].opts...))
			}
		}

		for chName, entries := range packageChannels[pkgName] {
			var chOpts []catalog.ChannelOption
			for _, e := range entries {
				if e.replaces != "" {
					chOpts = append(chOpts, catalog.Entry(e.version, catalog.Replaces(e.replaces)))
				} else {
					chOpts = append(chOpts, catalog.Entry(e.version))
				}
			}
			opts = append(opts, catalog.Channel(chName, chOpts...))
		}

		pkgOpts = append(pkgOpts, catalog.WithPackage(pkgName, opts...))
	}
	return pkgOpts, nil
}

// CatalogWithPackages builds a per-scenario catalog from a Gherkin data table.
// Table columns: package | version | channel | replaces | contents
func CatalogWithPackages(ctx context.Context, catalogUserName string, table *godog.Table) error {
	sc := Scenario(ctx)

	pkgOpts, err := parseCatalogTable(table)
	if err != nil {
		return err
	}

	cat := catalog.NewCatalog(catalogUserName, sc.ID, pkgOpts...)
	localRegistry, clusterRegistry, err := registryHosts()
	if err != nil {
		return err
	}

	result, err := cat.Build(ctx, "v1", localRegistry, clusterRegistry)
	if err != nil {
		return fmt.Errorf("failed to build catalog: %w", err)
	}

	sc.Catalogs[catalogUserName] = result.CatalogName
	for orig, param := range result.PackageNames {
		sc.CatalogPackageNames[orig] = param
	}

	catalogYAML := fmt.Sprintf(`apiVersion: olm.operatorframework.io/v1
kind: ClusterCatalog
metadata:
  name: %s
spec:
  priority: 0
  source:
    type: Image
    image:
      pollIntervalMinutes: 1
      ref: %s
`, result.CatalogName, result.CatalogImageRef)

	catalogObj, err := ToUnstructured(catalogYAML)
	if err != nil {
		return fmt.Errorf("failed to parse catalog YAML: %w", err)
	}
	injectTestAnnotations(catalogObj, sc)
	annotatedYAML, err := yaml.Marshal(catalogObj.Object)
	if err != nil {
		return fmt.Errorf("failed to marshal catalog YAML: %w", err)
	}

	if _, _, err := K8sCLIWithInput(ctx, string(annotatedYAML), "apply", "-f", "-"); err != nil {
		return fmt.Errorf("failed to apply ClusterCatalog: %w", err)
	}

	return nil
}

func parseContents(contents string) ([]catalog.BundleOption, error) {
	contents = strings.TrimSpace(contents)
	if contents == "" {
		return nil, nil
	}
	if strings.EqualFold(contents, "BadImage") {
		return []catalog.BundleOption{catalog.BadImage()}, nil
	}
	var opts []catalog.BundleOption
	for _, part := range strings.Split(contents, ",") {
		part = strings.TrimSpace(part)
		switch {
		case part == "CRD":
			opts = append(opts, catalog.WithCRD())
		case part == "Deployment":
			opts = append(opts, catalog.WithDeployment())
		case part == "ConfigMap":
			opts = append(opts, catalog.WithConfigMap())
		case strings.HasPrefix(part, "Property(") && strings.HasSuffix(part, ")"):
			// Property(type=value)
			inner := part[len("Property(") : len(part)-1]
			if k, v, ok := strings.Cut(inner, "="); ok {
				opts = append(opts, catalog.WithBundleProperty(k, v))
			}
		case strings.HasPrefix(part, "InstallMode(") && strings.HasSuffix(part, ")"):
			// InstallMode(SingleNamespace) or InstallMode(OwnNamespace)
			mode := part[len("InstallMode(") : len(part)-1]
			opts = append(opts, catalog.WithInstallMode(v1alpha1.InstallModeType(mode)))
		case strings.HasPrefix(part, "LargeCRD(") && strings.HasSuffix(part, ")"):
			// LargeCRD(250)
			countStr := part[len("LargeCRD(") : len(part)-1]
			count, err := strconv.ParseInt(countStr, 10, 0)
			if err != nil || count <= 0 {
				return nil, fmt.Errorf("invalid LargeCRD field count %q: must be a positive integer", countStr)
			}
			opts = append(opts, catalog.WithLargeCRD(int(count)))
		case strings.HasPrefix(part, "ClusterRegistry(") && strings.HasSuffix(part, ")"):
			// ClusterRegistry(mirrored-registry.operator-controller-e2e.svc.cluster.local:5000)
			host := part[len("ClusterRegistry(") : len(part)-1]
			opts = append(opts, catalog.WithClusterRegistry(host))
		case strings.HasPrefix(part, "StaticBundleDir(") && strings.HasSuffix(part, ")"):
			// StaticBundleDir(testdata/images/bundles/webhook-operator/v0.0.1)
			dir := part[len("StaticBundleDir(") : len(part)-1]
			absDir := filepath.Join(projectRootDir(), dir)
			opts = append(opts, catalog.StaticBundleDir(absDir))
		case strings.HasPrefix(part, "NSTemplate(") && strings.HasSuffix(part, ")"):
			// NSTemplate(privileged) or NSTemplate(baseline) or NSTemplate(restricted)
			level := part[len("NSTemplate(") : len(part)-1]
			opts = append(opts, catalog.WithNSTemplate(level))
		}
	}
	return opts, nil
}

// PrometheusMetricsAreReturned validates that each pod's stored metrics response is non-empty and parses as valid Prometheus text format.
func PrometheusMetricsAreReturned(ctx context.Context) error {
	sc := Scenario(ctx)
	for podName, mr := range sc.MetricsResponse {
		if mr == "" {
			return fmt.Errorf("metrics response is empty for pod %s", podName)
		}
		parser := expfmt.NewTextParser(model.UTF8Validation)
		metricsFamilies, err := parser.TextToMetricFamilies(strings.NewReader(mr))
		if err != nil {
			return fmt.Errorf("failed to parse metrics response for pod %s: %v", podName, err)
		}
		if len(metricsFamilies) == 0 {
			return fmt.Errorf("metrics response does not contain any metrics for pod %s", podName)
		}
	}
	return nil
}

// MarkDeploymentReadiness controls a deployment's readiness probe by removing or creating the readiness file in its pod.
func MarkDeploymentReadiness(ctx context.Context, deploymentName, state string) error {
	sc := Scenario(ctx)
	deploymentName = SubstituteScenarioVars(deploymentName, sc)
	v, err := K8sClient(ctx, "get", "deployment", "-n", sc.Namespace, deploymentName, "-o", "jsonpath={.spec.selector.matchLabels}")
	if err != nil {
		return err
	}
	var labels map[string]string
	if err := json.Unmarshal([]byte(v), &labels); err != nil {
		return err
	}
	podNameCmd := []string{"get", "pod", "-n", sc.Namespace, "-o", "jsonpath={.items[0].metadata.name}"}
	for k, v := range labels {
		podNameCmd = append(podNameCmd, fmt.Sprintf("--selector=%s=%s", k, v))
	}
	podName, err := K8sClient(ctx, podNameCmd...)
	if err != nil {
		return err
	}
	var op string
	switch state {
	case "not ready":
		op = "rm"
	case "ready":
		op = "touch"
	default:
		return fmt.Errorf("invalid state %s", state)
	}
	_, err = K8sClient(ctx, "exec", podName, "-n", sc.Namespace, "--", op, "/tmp/www/ready")
	return err
}

// SetCRDFieldMinValue patches a CRD to set the minimum value for a field.
// jsonPath is in the format ".spec.fieldName" and gets converted to the CRD schema path.
func SetCRDFieldMinValue(ctx context.Context, resourceType, jsonPath string, minValue int) error {
	var crdName string
	switch resourceType {
	case "ClusterExtension":
		crdName = "clusterextensions.olm.operatorframework.io"
	case "ClusterObjectSet":
		crdName = "clusterobjectsets.olm.operatorframework.io"
	case "ClusterObjectDeployment":
		crdName = "clusterobjectdeployments.olm.operatorframework.io"
	default:
		return fmt.Errorf("unsupported resource type: %s", resourceType)
	}

	// Convert JSON path like ".spec.progressDeadlineMinutes" to CRD schema path
	// e.g., ".spec.progressDeadlineMinutes" -> "properties/spec/properties/progressDeadlineMinutes"
	parts := strings.Split(strings.TrimPrefix(jsonPath, "."), ".")
	schemaParts := make([]string, 0, 2*len(parts))
	for _, part := range parts {
		schemaParts = append(schemaParts, "properties", part)
	}
	patchPath := fmt.Sprintf("/spec/versions/0/schema/openAPIV3Schema/%s/minimum", strings.Join(schemaParts, "/"))

	patch := fmt.Sprintf(`[{"op": "replace", "path": "%s", "value": %d}]`, patchPath, minValue)
	_, err := K8sClient(ctx, "patch", "crd", crdName, "--type=json", "-p", patch)
	return err
}

// templateYaml applies values to the template located in templatePath and returns the result or any errors reading
// the template file
func templateYaml(templatePath string, values map[string]string) (string, error) {
	yamlContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to read template file '%s': %v", templatePath, err)
	}
	return templateContent(string(yamlContent), values), nil
}

// templateContent applies values to content and returns the result
func templateContent(content string, values map[string]string) string {
	m := func(k string) string {
		if v, found := values[k]; found {
			return v
		}
		return "${" + k + "}"
	}

	return os.Expand(content, m)
}

// extendMap extends m with the key/values in keyValue, which is expected to be of even size
func extendMap(m map[string]string, keyValue ...string) map[string]string {
	if len(keyValue) > 0 {
		for i := 0; i < len(keyValue); i += 2 {
			m[keyValue[i]] = keyValue[i+1]
		}
	}
	return m
}

func GetResource(kind string, name string, namespace string) (*unstructured.Unstructured, error) {
	out, err := K8sClient(context.Background(), "get", kind, name, "-n", namespace, "-o", "yaml")
	if err != nil {
		return nil, err
	}
	obj, err := ToUnstructured(out)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

// ResolveSecretRef fetches an object from a Secret ref using kubectl.
func ResolveSecretRef(ref ocv1.SecretSourceRef) (*unstructured.Unstructured, error) {
	if ref.Namespace == "" {
		ref.Namespace = OLMNamespace
	}
	out, err := K8sClient(context.Background(), "get", "secret", ref.Name, "-n", ref.Namespace, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("getting Secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	var secret corev1.Secret
	if err := json.Unmarshal([]byte(out), &secret); err != nil {
		return nil, fmt.Errorf("unmarshaling Secret %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	data, ok := secret.Data[ref.Key]
	if !ok {
		return nil, fmt.Errorf("key %q not found in Secret %s/%s", ref.Key, ref.Namespace, ref.Name)
	}
	// Auto-detect gzip compression (magic bytes 0x1f 0x8b)
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("creating gzip reader for key %q in Secret %s/%s: %w", ref.Key, ref.Namespace, ref.Name, err)
		}
		defer reader.Close()
		const maxDecompressedSize = 10 * 1024 * 1024 // 10 MiB
		limited := io.LimitReader(reader, maxDecompressedSize+1)
		decompressed, err := io.ReadAll(limited)
		if err != nil {
			return nil, fmt.Errorf("decompressing key %q in Secret %s/%s: %w", ref.Key, ref.Namespace, ref.Name, err)
		}
		if len(decompressed) > maxDecompressedSize {
			return nil, fmt.Errorf("decompressed data for key %q in Secret %s/%s exceeds maximum size (%d bytes)", ref.Key, ref.Namespace, ref.Name, maxDecompressedSize)
		}
		data = decompressed
	}
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(data, &obj.Object); err != nil {
		return nil, fmt.Errorf("unmarshaling object from key %q in Secret %s/%s: %w", ref.Key, ref.Namespace, ref.Name, err)
	}
	return obj, nil
}

// AnnotationsAreAdded adds annotations from a data table to a resource using kubectl annotate.
// The table must have "key" and "value" columns.
// Only supports namespaced resources (always uses sc.Namespace).
func AnnotationsAreAdded(ctx context.Context, resourceName string, table *godog.Table) error {
	sc := Scenario(ctx)
	resourceName = SubstituteScenarioVars(resourceName, sc)

	pairs, err := ParseKeyValueTable(table, sc)
	if err != nil {
		return fmt.Errorf("invalid annotations table: %w", err)
	}

	kind, name, ok := strings.Cut(resourceName, "/")
	if !ok {
		return fmt.Errorf("invalid resource name format: %q (expected kind/name)", resourceName)
	}

	args := []string{"annotate", kind, name}
	for k, v := range pairs {
		args = append(args, fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, "--overwrite", "-n", sc.Namespace)
	if _, err := K8sClient(ctx, args...); err != nil {
		return fmt.Errorf("failed to annotate %s: %w; stderr: %s", resourceName, err, stderrOutput(err))
	}
	return nil
}

// LabelsAreAdded adds labels from a data table to a resource using kubectl label.
// The table must have "key" and "value" columns.
// Only supports namespaced resources (always uses sc.Namespace).
func LabelsAreAdded(ctx context.Context, resourceName string, table *godog.Table) error {
	sc := Scenario(ctx)
	resourceName = SubstituteScenarioVars(resourceName, sc)

	pairs, err := ParseKeyValueTable(table, sc)
	if err != nil {
		return fmt.Errorf("invalid labels table: %w", err)
	}

	kind, name, ok := strings.Cut(resourceName, "/")
	if !ok {
		return fmt.Errorf("invalid resource name format: %q (expected kind/name)", resourceName)
	}

	args := []string{"label", kind, name}
	for k, v := range pairs {
		args = append(args, fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, "--overwrite", "-n", sc.Namespace)
	if _, err := K8sClient(ctx, args...); err != nil {
		return fmt.Errorf("failed to label %s: %w; stderr: %s", resourceName, err, stderrOutput(err))
	}
	return nil
}

// ResourceHasAnnotations waits for a resource to have all annotations specified in the data table.
// The table must have "key" and "value" columns.
// Only supports namespaced resources (always uses sc.Namespace).
func ResourceHasAnnotations(ctx context.Context, resourceName string, table *godog.Table) error {
	sc := Scenario(ctx)
	resourceName = SubstituteScenarioVars(resourceName, sc)

	expected, err := ParseKeyValueTable(table, sc)
	if err != nil {
		return fmt.Errorf("invalid annotations table: %w", err)
	}

	kind, name, ok := strings.Cut(resourceName, "/")
	if !ok {
		return fmt.Errorf("invalid resource name format: %q (expected kind/name)", resourceName)
	}

	WaitFor(ctx, func() bool {
		out, err := K8sClient(ctx, "get", kind, name, "-n", sc.Namespace, "-o", "json")
		if err != nil {
			return false
		}
		var obj unstructured.Unstructured
		if err := json.Unmarshal([]byte(out), &obj); err != nil {
			return false
		}
		annotations := obj.GetAnnotations()
		for k, v := range expected {
			if actual, found := annotations[k]; !found || actual != v {
				logger.V(1).Info("Annotation not yet present or value mismatch", "resource", resourceName, "key", k, "expected", v, "actual", actual)
				return false
			}
		}
		return true
	})
	return nil
}

// MatchLabels checks that actual labels contain all expected key-value pairs.
// Returns the first mismatched key, the actual value, and false if a mismatch is found.
func MatchLabels(actual, expected map[string]string) (string, string, bool) {
	for k, v := range expected {
		if a, found := actual[k]; !found || a != v {
			return k, a, false
		}
	}
	return "", "", true
}

// ResourceHasLabels waits for a resource to have all labels specified in the data table.
// The table must have "key" and "value" columns.
// Only supports namespaced resources (always uses sc.Namespace).
func ResourceHasLabels(ctx context.Context, resourceName string, table *godog.Table) error {
	sc := Scenario(ctx)
	resourceName = SubstituteScenarioVars(resourceName, sc)

	expected, err := ParseKeyValueTable(table, sc)
	if err != nil {
		return fmt.Errorf("invalid labels table: %w", err)
	}

	kind, name, ok := strings.Cut(resourceName, "/")
	if !ok {
		return fmt.Errorf("invalid resource name format: %q (expected kind/name)", resourceName)
	}

	WaitFor(ctx, func() bool {
		out, err := K8sClient(ctx, "get", kind, name, "-n", sc.Namespace, "-o", "json")
		if err != nil {
			return false
		}
		var obj unstructured.Unstructured
		if err := json.Unmarshal([]byte(out), &obj); err != nil {
			return false
		}
		if key, got, ok := MatchLabels(obj.GetLabels(), expected); !ok {
			logger.V(1).Info("Label not yet present or value mismatch", "resource", resourceName, "key", key, "expected", expected[key], "actual", got)
			return false
		}
		return true
	})
	return nil
}

// NamespaceHasLabels waits for a namespace (cluster-scoped) to have all labels specified in the data table.
func NamespaceHasLabels(ctx context.Context, nsName string, table *godog.Table) error {
	sc := Scenario(ctx)
	nsName = SubstituteScenarioVars(nsName, sc)

	expected, err := ParseKeyValueTable(table, sc)
	if err != nil {
		return fmt.Errorf("invalid labels table: %w", err)
	}

	WaitFor(ctx, func() bool {
		out, err := K8sClient(ctx, "get", "namespace", nsName, "-o", "json")
		if err != nil {
			return false
		}
		var obj unstructured.Unstructured
		if err := json.Unmarshal([]byte(out), &obj); err != nil {
			return false
		}
		if key, got, ok := MatchLabels(obj.GetLabels(), expected); !ok {
			logger.V(1).Info("Namespace label not yet present or value mismatch", "namespace", nsName, "key", key, "expected", expected[key], "actual", got)
			return false
		}
		return true
	})
	return nil
}

// NamespaceDoesNotHaveLabel verifies a namespace does not have the specified label.
func NamespaceDoesNotHaveLabel(ctx context.Context, nsName string, labelKey string) error {
	sc := Scenario(ctx)
	nsName = SubstituteScenarioVars(nsName, sc)

	out, err := K8sClient(ctx, "get", "namespace", nsName, "-o", "json")
	if err != nil {
		return fmt.Errorf("failed to get namespace %q: %w", nsName, err)
	}
	var obj unstructured.Unstructured
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		return fmt.Errorf("failed to unmarshal namespace: %w", err)
	}
	if v, ok := obj.GetLabels()[labelKey]; ok {
		return fmt.Errorf("namespace %q has unexpected label %s=%s", nsName, labelKey, v)
	}
	return nil
}

// NamespaceHasNoOwnerReferences verifies a namespace remains unmanaged after installation.
func NamespaceHasNoOwnerReferences(ctx context.Context, nsName string) error {
	sc := Scenario(ctx)
	nsName = SubstituteScenarioVars(nsName, sc)

	out, err := K8sClient(ctx, "get", "namespace", nsName, "-o", "json")
	if err != nil {
		return fmt.Errorf("failed to get namespace %q: %w", nsName, err)
	}
	var obj unstructured.Unstructured
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		return fmt.Errorf("failed to unmarshal namespace: %w", err)
	}
	if refs := obj.GetOwnerReferences(); len(refs) != 0 {
		return fmt.Errorf("namespace %q has unexpected owner references: %v", nsName, refs)
	}
	return nil
}

// NestedString traverses a nested map[string]interface{} by the given keys
// and returns the leaf value as a string.
func NestedString(obj map[string]interface{}, keys ...string) (string, bool) {
	if len(keys) == 0 {
		return "", false
	}
	current := obj
	for _, k := range keys[:len(keys)-1] {
		next, ok := current[k].(map[string]interface{})
		if !ok {
			return "", false
		}
		current = next
	}
	v, ok := current[keys[len(keys)-1]].(string)
	return v, ok
}

// ParseKeyValueTable extracts key-value pairs from a godog data table.
// The table must have a header row with "key" and "value" columns.
// If sc is non-nil, variable substitution is applied to both keys and values.
func ParseKeyValueTable(table *godog.Table, sc *World) (map[string]string, error) {
	if len(table.Rows) < 2 {
		return nil, fmt.Errorf("table must have a header row and at least one data row")
	}

	header := table.Rows[0]
	keyIdx, valueIdx := -1, -1
	for i, cell := range header.Cells {
		switch cell.Value {
		case "key":
			keyIdx = i
		case "value":
			valueIdx = i
		}
	}
	if keyIdx == -1 || valueIdx == -1 {
		return nil, fmt.Errorf("table must have 'key' and 'value' columns")
	}

	maxIdx := keyIdx
	if valueIdx > maxIdx {
		maxIdx = valueIdx
	}

	result := make(map[string]string, len(table.Rows)-1)
	for rowIdx, row := range table.Rows[1:] {
		if len(row.Cells) <= maxIdx {
			return nil, fmt.Errorf("table row %d has %d cells, expected at least %d", rowIdx+2, len(row.Cells), maxIdx+1)
		}
		key := row.Cells[keyIdx].Value
		value := row.Cells[valueIdx].Value
		if sc != nil {
			key = SubstituteScenarioVars(key, sc)
			value = SubstituteScenarioVars(value, sc)
		}
		result[key] = value
	}
	return result, nil
}

// RolloutRestartIsPerformed runs "kubectl rollout restart deployment/<name>".
// See: https://github.com/operator-framework/operator-lifecycle-manager/issues/3392
func RolloutRestartIsPerformed(ctx context.Context, resourceName string) error {
	sc := Scenario(ctx)
	resourceName = SubstituteScenarioVars(resourceName, sc)

	kind, deploymentName, ok := strings.Cut(resourceName, "/")
	if !ok {
		return fmt.Errorf("invalid resource name format: %q (expected kind/name)", resourceName)
	}

	if kind != "deployment" {
		return fmt.Errorf("only deployment resources are supported for rollout restart, got: %q", kind)
	}

	// Run kubectl rollout restart to add the restart annotation.
	// This is the real command users run, so we test actual user behavior.
	out, err := K8sClient(ctx, "rollout", "restart", resourceName, "-n", sc.Namespace)
	if err != nil {
		return fmt.Errorf("failed to rollout restart %s: %w; stderr: %s", resourceName, err, stderrOutput(err))
	}

	logger.V(1).Info("Rollout restart initiated", "deployment", deploymentName, "output", out)

	return nil
}

// DeploymentPodTemplateHasAnnotation waits for the deployment's pod template to have
// the given annotation key. Uses JSON parsing to avoid JSONPath issues with dots
// in annotation keys. Polls with timeout.
func DeploymentPodTemplateHasAnnotation(ctx context.Context, deploymentName, annotationKey string) error {
	sc := Scenario(ctx)
	deploymentName = SubstituteScenarioVars(deploymentName, sc)

	WaitFor(ctx, func() bool {
		out, err := K8sClient(ctx, "get", "deployment", deploymentName, "-n", sc.Namespace, "-o", "json")
		if err != nil {
			return false
		}
		var d appsv1.Deployment
		if err := json.Unmarshal([]byte(out), &d); err != nil {
			return false
		}
		if v, found := d.Spec.Template.Annotations[annotationKey]; found {
			logger.V(1).Info("Pod template annotation found", "deployment", deploymentName, "key", annotationKey, "value", v)
			return true
		}
		logger.V(1).Info("Pod template annotation not yet present", "deployment", deploymentName, "key", annotationKey, "annotations", d.Spec.Template.Annotations)
		return false
	})
	return nil
}

// DeploymentRolloutIsComplete verifies that a deployment rollout has completed successfully.
// This ensures the new ReplicaSet is fully scaled up and the old one is scaled down.
func DeploymentRolloutIsComplete(ctx context.Context, deploymentName string) error {
	sc := Scenario(ctx)
	deploymentName = SubstituteScenarioVars(deploymentName, sc)

	WaitFor(ctx, func() bool {
		out, err := K8sClient(ctx, "rollout", "status", "deployment/"+deploymentName, "-n", sc.Namespace, "--watch=false")
		if err != nil {
			logger.V(1).Info("Failed to get rollout status", "deployment", deploymentName, "error", err)
			return false
		}
		// Successful rollout shows "successfully rolled out"
		if strings.Contains(out, "successfully rolled out") {
			logger.V(1).Info("Rollout completed successfully", "deployment", deploymentName)
			return true
		}
		logger.V(1).Info("Rollout not yet complete", "deployment", deploymentName, "status", out)
		return false
	})
	return nil
}

// DeploymentHasReplicaSets verifies that a deployment has the expected number of ReplicaSets
// and that at least one owned ReplicaSet is active with pods running.
func DeploymentHasReplicaSets(ctx context.Context, deploymentName string, expectedCount int) error {
	sc := Scenario(ctx)
	deploymentName = SubstituteScenarioVars(deploymentName, sc)

	WaitFor(ctx, func() bool {
		deploymentOut, err := K8sClient(ctx, "get", "deployment", deploymentName, "-n", sc.Namespace, "-o", "json")
		if err != nil {
			logger.V(1).Info("Failed to get deployment", "deployment", deploymentName, "error", err)
			return false
		}

		var deployment appsv1.Deployment
		if err := json.Unmarshal([]byte(deploymentOut), &deployment); err != nil {
			logger.V(1).Info("Failed to parse deployment", "error", err)
			return false
		}
		if !GenerationObserved(deployment.Generation, deployment.Status.ObservedGeneration) {
			return false
		}

		args := []string{"get", "rs", "-n", sc.Namespace}
		if deployment.Spec.Selector != nil && len(deployment.Spec.Selector.MatchLabels) > 0 {
			var parts []string
			for k, v := range deployment.Spec.Selector.MatchLabels {
				parts = append(parts, fmt.Sprintf("%s=%s", k, v))
			}
			args = append(args, "-l", strings.Join(parts, ","))
		}
		args = append(args, "-o", "json")
		out, err := K8sClient(ctx, args...)
		if err != nil {
			logger.V(1).Info("Failed to get ReplicaSets", "deployment", deploymentName, "error", err)
			return false
		}

		var rsCandidates struct {
			Items []appsv1.ReplicaSet `json:"items"`
		}
		if err := json.Unmarshal([]byte(out), &rsCandidates); err != nil {
			logger.V(1).Info("Failed to parse ReplicaSets", "error", err)
			return false
		}

		var rsList []appsv1.ReplicaSet
		for _, rs := range rsCandidates.Items {
			for _, owner := range rs.OwnerReferences {
				if owner.Kind == "Deployment" && owner.UID == deployment.UID {
					rsList = append(rsList, rs)
					break
				}
			}
		}

		if len(rsList) != expectedCount {
			logger.V(1).Info("ReplicaSet count does not match expected value yet", "deployment", deploymentName, "current", len(rsList), "expected", expectedCount)
			return false
		}

		// Verify at least one ReplicaSet has active replicas
		hasActiveRS := false
		for _, rs := range rsList {
			if GenerationObserved(rs.Generation, rs.Status.ObservedGeneration) && rs.Status.Replicas > 0 && rs.Status.ReadyReplicas > 0 {
				hasActiveRS = true
				logger.V(1).Info("Found active ReplicaSet", "name", rs.Name, "replicas", rs.Status.Replicas, "ready", rs.Status.ReadyReplicas)
			}
		}

		if !hasActiveRS {
			logger.V(1).Info("No active ReplicaSet found yet", "deployment", deploymentName)
			return false
		}

		logger.V(1).Info("ReplicaSet verification passed", "deployment", deploymentName, "count", len(rsList))
		return true
	})
	return nil
}
