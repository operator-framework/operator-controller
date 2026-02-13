package steps

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/cucumber/godog"
	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
	k8sresource "k8s.io/cli-runtime/pkg/resource"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

const (
	olmDeploymentName       = "operator-controller-controller-manager"
	timeout                 = 10 * time.Minute
	deploymentUpdateTimeout = 20 * time.Minute // Longer timeout for deployment updates via boxcutter
	tick                    = 1 * time.Second
)

var (
	olmNamespace   = "olmv1-system"
	kubeconfigPath string
	k8sCli         string
)

func RegisterSteps(sc *godog.ScenarioContext) {
	sc.Step(`^OLM is available$`, OLMisAvailable)
	sc.Step(`^(?i)bundle "([^"]+)" is installed in version "([^"]+)"$`, BundleInstalled)

	sc.Step(`^(?i)ClusterExtension is applied(?:\s+.*)?$`, ResourceIsApplied)
	sc.Step(`^(?i)ClusterExtension is updated to version "([^"]+)"$`, ClusterExtensionVersionUpdate)
	sc.Step(`^(?i)ClusterExtension is updated(?:\s+.*)?$`, ResourceIsApplied)
	sc.Step(`^(?i)ClusterExtension is available$`, ClusterExtensionIsAvailable)
	sc.Step(`^(?i)ClusterExtension is rolled out$`, ClusterExtensionIsRolledOut)
	sc.Step(`^(?i)ClusterExtension resources are created and labeled$`, ClusterExtensionResourcesCreatedAndAreLabeled)
	sc.Step(`^(?i)ClusterExtension is removed$`, ClusterExtensionIsRemoved)
	sc.Step(`^(?i)ClusterExtension (?:latest generation )?has (?:been )?reconciled(?: the latest generation)?$`, ClusterExtensionReconciledLatestGeneration)
	sc.Step(`^(?i)the ClusterExtension's constituent resources are removed$`, ClusterExtensionResourcesRemoved)
	sc.Step(`^(?i)ClusterExtension reports "([^"]+)" as active revision(s?)$`, ClusterExtensionReportsActiveRevisions)
	sc.Step(`^(?i)ClusterExtension reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+) and Message:$`, ClusterExtensionReportsCondition)
	sc.Step(`^(?i)ClusterExtension reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+) and Message includes:$`, ClusterExtensionReportsConditionWithMessageFragment)
	sc.Step(`^(?i)ClusterExtension reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+)$`, ClusterExtensionReportsConditionWithoutMsg)
	sc.Step(`^(?i)ClusterExtension reports ([[:alnum:]]+) as ([[:alnum:]]+)$`, ClusterExtensionReportsConditionWithoutReason)
	sc.Step(`^(?i)ClusterExtensionRevision "([^"]+)" reports ([[:alnum:]]+) as ([[:alnum:]]+) with Reason ([[:alnum:]]+)$`, ClusterExtensionRevisionReportsConditionWithoutMsg)
	sc.Step(`^(?i)ClusterExtension reports ([[:alnum:]]+) transition between (\d+) and (\d+) minutes since its creation$`, ClusterExtensionReportsConditionTransitionTime)
	sc.Step(`^(?i)ClusterExtensionRevision "([^"]+)" is archived$`, ClusterExtensionRevisionIsArchived)
	sc.Step(`^(?i)ClusterExtensionRevision "([^"]+)" contains annotation "([^"]+)" with value$`, ClusterExtensionRevisionHasAnnotationWithValue)
	sc.Step(`^(?i)ClusterExtensionRevision "([^"]+)" has label "([^"]+)" with value "([^"]+)"$`, ClusterExtensionRevisionHasLabelWithValue)

	sc.Step(`^(?i)resource "([^"]+)" is installed$`, ResourceAvailable)
	sc.Step(`^(?i)resource "([^"]+)" is available$`, ResourceAvailable)
	sc.Step(`^(?i)resource "([^"]+)" is removed$`, ResourceRemoved)
	sc.Step(`^(?i)resource "([^"]+)" is eventually not found$`, ResourceEventuallyNotFound)
	sc.Step(`^(?i)resource "([^"]+)" exists$`, ResourceAvailable)
	sc.Step(`^(?i)resource is applied$`, ResourceIsApplied)
	sc.Step(`^(?i)resource "deployment/test-operator" reports as (not ready|ready)$`, MarkTestOperatorNotReady)

	sc.Step(`^(?i)resource apply fails with error msg containing "([^"]+)"$`, ResourceApplyFails)
	sc.Step(`^(?i)resource "([^"]+)" is eventually restored$`, ResourceRestored)
	sc.Step(`^(?i)resource "([^"]+)" matches$`, ResourceMatches)

	sc.Step(`^(?i)ServiceAccount "([^"]*)" with needed permissions is available in test namespace$`, ServiceAccountWithNeededPermissionsIsAvailableInNamespace)
	sc.Step(`^(?i)ServiceAccount "([^"]*)" with needed permissions is available in \${TEST_NAMESPACE}$`, ServiceAccountWithNeededPermissionsIsAvailableInNamespace)
	sc.Step(`^(?i)ServiceAccount "([^"]*)" is available in \${TEST_NAMESPACE}$`, ServiceAccountIsAvailableInNamespace)
	sc.Step(`^(?i)ServiceAccount "([^"]*)" in test namespace is cluster admin$`, ServiceAccountWithClusterAdminPermissionsIsAvailableInNamespace)
	sc.Step(`^(?i)ServiceAccount "([^"]+)" in test namespace has permissions to fetch "([^"]+)" metrics$`, ServiceAccountWithFetchMetricsPermissions)
	sc.Step(`^(?i)ServiceAccount "([^"]+)" sends request to "([^"]+)" endpoint of "([^"]+)" service$`, SendMetricsRequest)

	sc.Step(`^"([^"]+)" catalog is updated to version "([^"]+)"$`, CatalogIsUpdatedToVersion)
	sc.Step(`^(?i)ClusterCatalog "([^"]+)" is updated to version "([^"]+)"$`, CatalogIsUpdatedToVersion)
	sc.Step(`^"([^"]+)" catalog serves bundles$`, CatalogServesBundles)
	sc.Step(`^(?i)ClusterCatalog "([^"]+)" serves bundles$`, CatalogServesBundles)
	sc.Step(`^"([^"]+)" catalog image version "([^"]+)" is also tagged as "([^"]+)"$`, TagCatalogImage)
	sc.Step(`^(?i)ClusterCatalog "([^"]+)" image version "([^"]+)" is also tagged as "([^"]+)"$`, TagCatalogImage)
	sc.Step(`^(?i)ClusterCatalog "([^"]+)" is deleted$`, CatalogIsDeleted)

	sc.Step(`^(?i)operator "([^"]+)" target namespace is "([^"]+)"$`, OperatorTargetNamespace)
	sc.Step(`^(?i)Prometheus metrics are returned in the response$`, PrometheusMetricsAreReturned)

	sc.Step(`^(?i)min value for (ClusterExtension|ClusterExtensionRevision) ((?:\.[a-zA-Z]+)+) is set to (\d+)$`, SetCRDFieldMinValue)

	sc.Step(`^(?i)resource "([^"]+)" has no proxy environment variables$`, ResourceHasNoProxyEnvVars)
	sc.Step(`^(?i)ClusterExtension revision manifests have no proxy environment variables$`, ClusterExtensionRevisionHasNoProxyEnvVars)
	sc.Step(`^(?i)operator-controller has environment variable "([^"]+)" set to "([^"]+)"$`, OperatorControllerHasEnvVar)
	sc.Step(`^(?i)resource "([^"]+)" has environment variable "([^"]+)" set to "([^"]+)"$`, ResourceHasEnvVar)
	sc.Step(`^(?i)operator-controller has environment variable "([^"]+)" removed$`, OperatorControllerEnvVarRemoved)
	sc.Step(`^(?i)resource "([^"]+)" has no environment variable "([^"]+)"$`, ResourceHasNoEnvVar)
}

func init() {
	flagSet := pflag.CommandLine
	flagSet.StringVar(&k8sCli, "k8s.cli", "kubectl", "Path to k8s cli")
	if v, found := os.LookupEnv("KUBECONFIG"); found {
		kubeconfigPath = v
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			panic(fmt.Sprintf("cannot determine user home directory: %v", err))
		}
		flagSet.StringVar(&kubeconfigPath, "kubeconfig", filepath.Join(home, ".kube", "config"), "Paths to a kubeconfig. Only required if out-of-cluster.")
	}
}

func k8sClient(args ...string) (string, error) {
	cmd := exec.Command(k8sCli, args...)
	logger.V(1).Info("Running", "command", strings.Join(cmd.Args, " "))
	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))
	b, err := cmd.Output()
	if err != nil {
		logger.V(1).Info("Failed to run", "command", strings.Join(cmd.Args, " "), "stderr", stderrOutput(err), "error", err)
	}
	output := string(b)
	logger.V(1).Info("Output", "command", strings.Join(cmd.Args, " "), "output", output)
	return output, err
}

func k8scliWithInput(yaml string, args ...string) (string, error) {
	cmd := exec.Command(k8sCli, args...)
	cmd.Stdin = bytes.NewBufferString(yaml)
	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))
	b, err := cmd.Output()
	return string(b), err
}

func OLMisAvailable(ctx context.Context) error {
	require.Eventually(godog.T(ctx), func() bool {
		v, err := k8sClient("get", "deployment", "-n", olmNamespace, olmDeploymentName, "-o", "jsonpath='{.status.conditions[?(@.type==\"Available\")].status}'")
		if err != nil {
			return false
		}
		return v == "'True'"
	}, timeout, tick)
	return nil
}

func BundleInstalled(ctx context.Context, name, version string) error {
	sc := scenarioCtx(ctx)
	waitFor(ctx, func() bool {
		v, err := k8sClient("get", "clusterextension", sc.clusterExtensionName, "-o", "jsonpath={.status.install.bundle}")
		if err != nil {
			return false
		}
		var bundle map[string]interface{}
		if err := json.Unmarshal([]byte(v), &bundle); err != nil {
			return false
		}
		return bundle["name"] == name && bundle["version"] == version
	})
	return nil
}

func toUnstructured(yamlContent string) (*unstructured.Unstructured, error) {
	var u map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &u); err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: u}, nil
}

func substituteScenarioVars(content string, sc *scenarioContext) string {
	vars := map[string]string{
		"TEST_NAMESPACE": sc.namespace,
		"NAME":           sc.clusterExtensionName,
		"CATALOG_IMG":    "docker-registry.operator-controller-e2e.svc.cluster.local:5000/e2e/test-catalog:v1",
	}
	if v, found := os.LookupEnv("CATALOG_IMG"); found {
		vars["CATALOG_IMG"] = v
	}
	return templateContent(content, vars)
}

func ResourceApplyFails(ctx context.Context, errMsg string, yamlTemplate *godog.DocString) error {
	sc := scenarioCtx(ctx)
	yamlContent := substituteScenarioVars(yamlTemplate.Content, sc)
	_, err := toUnstructured(yamlContent)
	if err != nil {
		return fmt.Errorf("failed to parse resource yaml: %v", err)
	}
	waitFor(ctx, func() bool {
		_, err := k8scliWithInput(yamlContent, "apply", "-f", "-")
		if err == nil {
			return false
		}
		if stdErr := stderrOutput(err); !strings.Contains(stdErr, errMsg) {
			return false
		}
		return true
	})
	return nil
}

func ClusterExtensionVersionUpdate(ctx context.Context, version string) error {
	sc := scenarioCtx(ctx)
	patch := map[string]any{
		"spec": map[string]any{
			"source": map[string]any{
				"catalog": map[string]any{
					"version": version,
				},
			},
		},
	}
	pb, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = k8sClient("patch", "clusterextension", sc.clusterExtensionName, "--type", "merge", "-p", string(pb))
	return err
}

func ResourceIsApplied(ctx context.Context, yamlTemplate *godog.DocString) error {
	sc := scenarioCtx(ctx)
	yamlContent := substituteScenarioVars(yamlTemplate.Content, sc)
	res, err := toUnstructured(yamlContent)
	if err != nil {
		return fmt.Errorf("failed to parse resource yaml: %v", err)
	}
	out, err := k8scliWithInput(yamlContent, "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("failed to apply resource %v %w", out, err)
	}
	if res.GetKind() == "ClusterExtension" {
		sc.clusterExtensionName = res.GetName()
	}
	return nil
}

func ClusterExtensionIsAvailable(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	require.Eventually(godog.T(ctx), func() bool {
		v, err := k8sClient("get", "clusterextension", sc.clusterExtensionName, "-o", "jsonpath={.status.conditions[?(@.type==\"Installed\")].status}")
		if err != nil {
			return false
		}
		return v == "True"
	}, timeout, tick)
	return nil
}

func ClusterExtensionReconciledLatestGeneration(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	waitFor(ctx, func() bool {
		// Get both generation and observedGeneration in a single kubectl call
		output, err := k8sClient("get", "clusterextension", sc.clusterExtensionName,
			"-o", "jsonpath={.metadata.generation},{.status.conditions[?(@.type=='Progressing')].observedGeneration}")
		if err != nil || output == "" {
			return false
		}
		parts := strings.Split(output, ",")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return false
		}
		// Both exist and are equal means reconciliation happened
		return parts[0] == parts[1]
	})
	return nil
}

func ClusterExtensionIsRolledOut(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	require.Eventually(godog.T(ctx), func() bool {
		v, err := k8sClient("get", "clusterextension", sc.clusterExtensionName, "-o", "jsonpath={.status.conditions[?(@.type==\"Progressing\")]}")
		if err != nil {
			return false
		}

		var condition map[string]interface{}
		if err := json.Unmarshal([]byte(v), &condition); err != nil {
			return false
		}

		return condition["status"] == "True" && condition["reason"] == "Succeeded" && condition["type"] == "Progressing"
	}, timeout, tick)

	// Save ClusterExtension resources to test context for posterior checks
	if err := sc.GatherClusterExtensionObjects(); err != nil {
		return err
	}
	return nil
}

func ClusterExtensionResourcesCreatedAndAreLabeled(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	if len(sc.GetClusterExtensionObjects()) == 0 {
		return fmt.Errorf("extension objects not found in context")
	}

	for _, obj := range sc.extensionObjects {
		waitFor(ctx, func() bool {
			kind := obj.GetObjectKind().GroupVersionKind().Kind
			clusterObj, err := getResource(kind, obj.GetName(), obj.GetNamespace())
			if err != nil {
				logger.V(1).Error(err, "error getting resource", "name", obj.GetName(), "namespace", obj.GetNamespace(), "kind", kind)
				return false
			}

			labels := clusterObj.GetLabels()
			if labels == nil {
				logger.V(1).Info("no labels found for resource", "name", obj.GetName(), "namespace", obj.GetNamespace(), "kind", kind)
				return false
			}

			for key, expectedValue := range map[string]string{
				"olm.operatorframework.io/owner-kind": "ClusterExtension",
				"olm.operatorframework.io/owner-name": sc.clusterExtensionName,
			} {
				if labels[key] != expectedValue {
					logger.V(1).Info("invalid resource label value", "name", obj.GetName(), "namespace", obj.GetNamespace(), "kind", kind, "label", key, "expected", expectedValue, "actual", labels["olm.operatorframework.io/owner-kind"])
					return false
				}
			}
			return true
		})
	}
	return nil
}

func ClusterExtensionIsRemoved(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	return ResourceRemoved(ctx, fmt.Sprintf("clusterextension/%s", sc.clusterExtensionName))
}

func ClusterExtensionResourcesRemoved(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	if len(sc.GetClusterExtensionObjects()) == 0 {
		return fmt.Errorf("extension objects not found in context")
	}
	for _, obj := range sc.extensionObjects {
		if err := ResourceEventuallyNotFound(ctx, fmt.Sprintf("%s/%s", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())); err != nil {
			return err
		}
	}

	// Wait for CRDs owned by this ClusterExtension to be fully deleted
	// This is critical to avoid CRD ownership conflicts when running multiple test scenarios
	// that install the same bundle (which creates the same cluster-scoped CRDs)
	logger.Info("waiting for CRDs owned by ClusterExtension to be deleted", "clusterExtension", sc.clusterExtensionName)

	// Use a longer timeout for CRD deletion since finalizers and cleanup can take time
	crdTimeout := 2 * timeout // Double the normal timeout for CRD deletion
	require.Eventually(godog.T(ctx), func() bool {
		// Get all CRDs that are owned by this ClusterExtension
		output, err := k8sClient("get", "crd", "-l", fmt.Sprintf("olm.operatorframework.io/owner-name=%s", sc.clusterExtensionName), "-o", "jsonpath={.items[*].metadata.name}")
		if err != nil {
			logger.V(1).Info("error checking for CRDs", "error", err)
			return false
		}

		// If output is empty, all CRDs are deleted
		crdNames := strings.TrimSpace(output)
		if crdNames == "" {
			logger.Info("all CRDs owned by ClusterExtension have been deleted", "clusterExtension", sc.clusterExtensionName)
			return true
		}

		logger.V(1).Info("still waiting for CRDs to be deleted", "crds", crdNames)
		return false
	}, crdTimeout, tick, "CRDs owned by ClusterExtension not deleted after timeout")

	return nil
}

func waitFor(ctx context.Context, conditionFn func() bool) {
	require.Eventually(godog.T(ctx), conditionFn, timeout, tick)
}

func waitForWithTimeout(ctx context.Context, conditionFn func() bool, customTimeout time.Duration) {
	require.Eventually(godog.T(ctx), conditionFn, customTimeout, tick)
}

type msgMatchFn func(string) bool

func alwaysMatch(_ string) bool { return true }

func waitForCondition(ctx context.Context, resourceType, resourceName, conditionType, conditionStatus string, conditionReason *string, msgCmp msgMatchFn) error {
	require.Eventually(godog.T(ctx), func() bool {
		v, err := k8sClient("get", resourceType, resourceName, "-o", fmt.Sprintf("jsonpath={.status.conditions[?(@.type==\"%s\")]}", conditionType))
		if err != nil {
			return false
		}

		var condition metav1.Condition
		if err := json.Unmarshal([]byte(v), &condition); err != nil {
			return false
		}
		if condition.Status != metav1.ConditionStatus(conditionStatus) {
			return false
		}
		if conditionReason != nil && condition.Reason != *conditionReason {
			return false
		}
		if msgCmp != nil && !msgCmp(condition.Message) {
			return false
		}

		return true
	}, timeout, tick)
	return nil
}

func waitForExtensionCondition(ctx context.Context, conditionType, conditionStatus string, conditionReason *string, msgCmp msgMatchFn) error {
	sc := scenarioCtx(ctx)
	return waitForCondition(ctx, "clusterextension", sc.clusterExtensionName, conditionType, conditionStatus, conditionReason, msgCmp)
}

// waitForOperatorControllerStartup waits for the operator-controller pod to be Ready
// and for the startup hook to complete. This ensures the manager cache is synced
// and the controller is ready to handle new ClusterExtensions with the current configuration.
func waitForOperatorControllerStartup(ctx context.Context) {
	// Wait for the operator-controller pod to be Running and Ready
	var podName string
	var podStartTime *metav1.Time
	waitFor(ctx, func() bool {
		raw, err := k8sClient("get", "pods", "-n", olmNamespace, "-l", "app.kubernetes.io/name=operator-controller", "-o", "json")
		if err != nil {
			return false
		}
		var podList corev1.PodList
		if err := json.Unmarshal([]byte(raw), &podList); err != nil {
			return false
		}
		if len(podList.Items) == 0 {
			return false
		}

		// Find a Running and Ready pod (preferably the newest by start time)
		var readyPod *corev1.Pod
		for i := range podList.Items {
			pod := &podList.Items[i]
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}

			// Check if pod is Ready
			isReady := false
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					isReady = true
					break
				}
			}

			if isReady {
				// Select this pod if we don't have one yet, or if it's newer than the current selection
				if readyPod == nil || (pod.Status.StartTime != nil && readyPod.Status.StartTime != nil && pod.Status.StartTime.After(readyPod.Status.StartTime.Time)) {
					readyPod = pod
				}
			}
		}

		if readyPod != nil {
			podName = readyPod.Name
			podStartTime = readyPod.Status.StartTime
			return true
		}
		return false
	})

	// Wait for the startup hook to complete
	// The startup hook logs "triggered reconciliation for existing ClusterExtensions" after processing
	// We check all logs since pod start to ensure we don't miss the message
	waitFor(ctx, func() bool {
		// Build kubectl logs arguments conditionally based on whether we have a start time
		args := []string{"logs", "-n", olmNamespace, podName}
		if podStartTime != nil {
			args = append(args, "--since-time="+podStartTime.Format(time.RFC3339))
		}

		logs, err := k8sClient(args...)
		if err != nil {
			return false
		}
		// Look for the log message that indicates startup hook completion
		return strings.Contains(logs, "triggered reconciliation for existing ClusterExtensions")
	})
}

func ClusterExtensionReportsCondition(ctx context.Context, conditionType, conditionStatus, conditionReason string, msg *godog.DocString) error {
	msgCmp := alwaysMatch
	if msg != nil {
		expectedMsg := substituteScenarioVars(strings.Join(strings.Fields(msg.Content), " "), scenarioCtx(ctx))
		msgCmp = func(actual string) bool {
			return actual == expectedMsg
		}
	}
	return waitForExtensionCondition(ctx, conditionType, conditionStatus, &conditionReason, msgCmp)
}

func ClusterExtensionReportsConditionWithMessageFragment(ctx context.Context, conditionType, conditionStatus, conditionReason string, msgFragment *godog.DocString) error {
	msgCmp := alwaysMatch
	if msgFragment != nil {
		expectedMsgFragment := substituteScenarioVars(strings.Join(strings.Fields(msgFragment.Content), " "), scenarioCtx(ctx))
		msgCmp = func(actualMsg string) bool {
			return strings.Contains(actualMsg, expectedMsgFragment)
		}
	}
	return waitForExtensionCondition(ctx, conditionType, conditionStatus, &conditionReason, msgCmp)
}

func ClusterExtensionReportsConditionWithoutMsg(ctx context.Context, conditionType, conditionStatus, conditionReason string) error {
	return ClusterExtensionReportsCondition(ctx, conditionType, conditionStatus, conditionReason, nil)
}

func ClusterExtensionReportsConditionWithoutReason(ctx context.Context, conditionType, conditionStatus string) error {
	return waitForExtensionCondition(ctx, conditionType, conditionStatus, nil, nil)
}

func ClusterExtensionReportsConditionTransitionTime(ctx context.Context, conditionType string, minMinutes, maxMinutes int) error {
	sc := scenarioCtx(ctx)
	t := godog.T(ctx)

	// Get the ClusterExtension's creation timestamp and condition's lastTransitionTime
	v, err := k8sClient("get", "clusterextension", sc.clusterExtensionName, "-o",
		fmt.Sprintf("jsonpath={.metadata.creationTimestamp},{.status.conditions[?(@.type==\"%s\")].lastTransitionTime}", conditionType))
	require.NoError(t, err)

	parts := strings.Split(v, ",")
	require.Len(t, parts, 2, "expected creationTimestamp and lastTransitionTime but got: %s", v)

	creationTimestamp, err := time.Parse(time.RFC3339, parts[0])
	require.NoError(t, err, "failed to parse creationTimestamp")

	lastTransitionTime, err := time.Parse(time.RFC3339, parts[1])
	require.NoError(t, err, "failed to parse lastTransitionTime")

	transitionDuration := lastTransitionTime.Sub(creationTimestamp)
	minDuration := time.Duration(minMinutes) * time.Minute
	maxDuration := time.Duration(maxMinutes) * time.Minute

	require.GreaterOrEqual(t, transitionDuration, minDuration,
		"condition %s transitioned too early: %v since creation (expected >= %v)", conditionType, transitionDuration, minDuration)
	require.LessOrEqual(t, transitionDuration, maxDuration,
		"condition %s transitioned too late: %v since creation (expected <= %v)", conditionType, transitionDuration, maxDuration)

	return nil
}

func ClusterExtensionReportsActiveRevisions(ctx context.Context, rawRevisionNames string) error {
	sc := scenarioCtx(ctx)
	expectedRevisionNames := sets.New[string]()
	for _, rev := range strings.Split(rawRevisionNames, ",") {
		expectedRevisionNames.Insert(substituteScenarioVars(strings.TrimSpace(rev), sc))
	}

	waitFor(ctx, func() bool {
		v, err := k8sClient("get", "clusterextension", sc.clusterExtensionName, "-o", "jsonpath={.status.activeRevisions}")
		if err != nil {
			return false
		}
		var activeRevisions []ocv1.RevisionStatus
		if err := json.Unmarshal([]byte(v), &activeRevisions); err != nil {
			return false
		}
		activeRevisionsNames := sets.New[string]()
		for _, rev := range activeRevisions {
			activeRevisionsNames.Insert(rev.Name)
		}
		return activeRevisionsNames.Equal(expectedRevisionNames)
	})
	return nil
}

func ClusterExtensionRevisionReportsConditionWithoutMsg(ctx context.Context, revisionName, conditionType, conditionStatus, conditionReason string) error {
	return waitForCondition(ctx, "clusterextensionrevision", substituteScenarioVars(revisionName, scenarioCtx(ctx)), conditionType, conditionStatus, &conditionReason, nil)
}

func ClusterExtensionRevisionIsArchived(ctx context.Context, revisionName string) error {
	return waitForCondition(ctx, "clusterextensionrevision", substituteScenarioVars(revisionName, scenarioCtx(ctx)), "Progressing", "False", ptr.To("Archived"), nil)
}

func ClusterExtensionRevisionHasAnnotationWithValue(ctx context.Context, revisionName, annotationKey string, annotationValue *godog.DocString) error {
	sc := scenarioCtx(ctx)
	revisionName = substituteScenarioVars(strings.TrimSpace(revisionName), sc)
	expectedValue := ""
	if annotationValue != nil {
		expectedValue = annotationValue.Content
	}
	waitFor(ctx, func() bool {
		obj, err := getResource("clusterextensionrevision", revisionName, "")
		if err != nil {
			logger.V(1).Error(err, "failed to get clusterextensionrevision", "name", revisionName)
			return false
		}
		if obj.GetAnnotations() == nil {
			return false
		}
		return obj.GetAnnotations()[annotationKey] == expectedValue
	})
	return nil
}

func ClusterExtensionRevisionHasLabelWithValue(ctx context.Context, revisionName, labelKey, labelValue string) error {
	sc := scenarioCtx(ctx)
	revisionName = substituteScenarioVars(strings.TrimSpace(revisionName), sc)
	labelValue = substituteScenarioVars(labelValue, sc)
	waitFor(ctx, func() bool {
		obj, err := getResource("clusterextensionrevision", revisionName, "")
		if err != nil {
			logger.V(1).Error(err, "failed to get clusterextensionrevision", "name", revisionName)
			return false
		}
		if obj.GetLabels() == nil {
			return false
		}
		return obj.GetLabels()[labelKey] == labelValue
	})
	return nil
}

func ResourceAvailable(ctx context.Context, resource string) error {
	sc := scenarioCtx(ctx)
	resource = substituteScenarioVars(resource, sc)
	kind, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <kind>/<name>", resource)
	}
	waitFor(ctx, func() bool {
		_, err := k8sClient("get", kind, name, "-n", sc.namespace)
		return err == nil
	})
	return nil
}

func ResourceRemoved(ctx context.Context, resource string) error {
	sc := scenarioCtx(ctx)
	resource = substituteScenarioVars(resource, sc)
	kind, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <kind>/<name>", resource)
	}
	yaml, err := k8sClient("get", kind, name, "-n", sc.namespace, "-o", "yaml")
	if err != nil {
		return err
	}
	obj, err := toUnstructured(yaml)
	if err != nil {
		return err
	}
	sc.removedResources = append(sc.removedResources, *obj)
	_, err = k8sClient("delete", kind, name, "-n", sc.namespace)
	return err
}

func ResourceEventuallyNotFound(ctx context.Context, resource string) error {
	sc := scenarioCtx(ctx)
	resource = substituteScenarioVars(resource, sc)
	kind, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <kind>/<name>", resource)
	}

	waitFor(ctx, func() bool {
		obj, err := k8sClient("get", kind, name, "-n", sc.namespace, "--ignore-not-found", "-o", "yaml")
		return err == nil && strings.TrimSpace(obj) == ""
	})
	return nil
}

func ResourceMatches(ctx context.Context, resource string, requiredContentTemplate *godog.DocString) error {
	sc := scenarioCtx(ctx)
	resource = substituteScenarioVars(resource, sc)
	kind, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <kind>/<name>", resource)
	}
	requiredContent, err := toUnstructured(substituteScenarioVars(requiredContentTemplate.Content, sc))
	if err != nil {
		return fmt.Errorf("failed to parse required resource yaml: %v", err)
	}
	waitFor(ctx, func() bool {
		objJson, err := k8sClient("get", kind, name, "-n", sc.namespace, "-o", "json")
		if err != nil {
			return false
		}
		obj, err := toUnstructured(objJson)
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
		upd, err := toUnstructured(string(updJson))
		if err != nil {
			return false
		}

		return len(cmp.Diff(upd.Object, obj.Object)) == 0
	})
	return nil
}

func ResourceRestored(ctx context.Context, resource string) error {
	sc := scenarioCtx(ctx)
	resource = substituteScenarioVars(resource, sc)
	kind, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <kind>/<name>", resource)
	}
	waitFor(ctx, func() bool {
		yaml, err := k8sClient("get", kind, name, "-n", sc.namespace, "-o", "yaml")
		if err != nil {
			return false
		}
		obj, err := toUnstructured(yaml)
		if err != nil {
			return false
		}
		ct := obj.GetCreationTimestamp()

		for i, removed := range sc.removedResources {
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
				sc.removedResources = append(sc.removedResources[:i], sc.removedResources[i+1:]...)
				return true
			}
		}
		return false
	})
	return nil
}

func applyServiceAccount(ctx context.Context, serviceAccount string) error {
	sc := scenarioCtx(ctx)
	vars := extendMap(map[string]string{
		"TEST_NAMESPACE":       sc.namespace,
		"SERVICE_ACCOUNT_NAME": serviceAccount,
		"SERVICEACCOUNT_NAME":  serviceAccount,
	})

	yaml, err := templateYaml(filepath.Join("steps", "testdata", "serviceaccount-template.yaml"), vars)
	if err != nil {
		return fmt.Errorf("failed to template ServiceAccount yaml: %v", err)
	}

	// Apply the ServiceAccount configuration
	_, err = k8scliWithInput(yaml, "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("failed to apply ServiceAccount configuration: %v: %s", err, stderrOutput(err))
	}

	return nil
}

func applyPermissionsToServiceAccount(ctx context.Context, serviceAccount, rbacTemplate string, keyValue ...string) error {
	sc := scenarioCtx(ctx)
	if err := applyServiceAccount(ctx, serviceAccount); err != nil {
		return err
	}
	vars := extendMap(map[string]string{
		"TEST_NAMESPACE":         sc.namespace,
		"SERVICE_ACCOUNT_NAME":   serviceAccount,
		"SERVICEACCOUNT_NAME":    serviceAccount,
		"CLUSTER_EXTENSION_NAME": sc.clusterExtensionName,
		"CLUSTEREXTENSION_NAME":  sc.clusterExtensionName,
	}, keyValue...)

	yaml, err := templateYaml(filepath.Join("steps", "testdata", rbacTemplate), vars)
	if err != nil {
		return fmt.Errorf("failed to template RBAC yaml: %v", err)
	}

	// Apply the RBAC configuration
	_, err = k8scliWithInput(yaml, "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("failed to apply RBAC configuration: %v: %s", err, stderrOutput(err))
	}

	return nil
}

func ServiceAccountIsAvailableInNamespace(ctx context.Context, serviceAccount string) error {
	return applyServiceAccount(ctx, serviceAccount)
}

func ServiceAccountWithNeededPermissionsIsAvailableInNamespace(ctx context.Context, serviceAccount string) error {
	return applyPermissionsToServiceAccount(ctx, serviceAccount, "rbac-template.yaml")
}

func ServiceAccountWithClusterAdminPermissionsIsAvailableInNamespace(ctx context.Context, serviceAccount string) error {
	return applyPermissionsToServiceAccount(ctx, serviceAccount, "cluster-admin-rbac-template.yaml")
}

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

func randomAvailablePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func SendMetricsRequest(ctx context.Context, serviceAccount string, endpoint string, controllerName string) error {
	sc := scenarioCtx(ctx)
	serviceNs, err := k8sClient("get", "service", "-A", "-o", fmt.Sprintf(`jsonpath={.items[?(@.metadata.name=="%s-service")].metadata.namespace}`, controllerName))
	if err != nil {
		return err
	}
	v, err := k8sClient("get", "service", "-n", serviceNs, fmt.Sprintf("%s-service", controllerName), "-o", "json")
	if err != nil {
		return err
	}
	var service corev1.Service
	if err := json.Unmarshal([]byte(v), &service); err != nil {
		return err
	}
	podNameCmd := []string{"get", "pod", "-n", olmNamespace, "-o", "jsonpath={.items}"}
	for k, v := range service.Spec.Selector {
		podNameCmd = append(podNameCmd, fmt.Sprintf("--selector=%s=%s", k, v))
	}
	v, err = k8sClient(podNameCmd...)
	if err != nil {
		return err
	}

	var pods []corev1.Pod
	if err := json.Unmarshal([]byte(v), &pods); err != nil {
		return err
	}
	token, err := k8sClient("create", "token", serviceAccount, "-n", sc.namespace)
	if err != nil {
		return err
	}
	var metricsPort int32
	for _, p := range service.Spec.Ports {
		if p.Name == "metrics" {
			metricsPort = p.Port
			break
		}
	}
	sc.metricsResponse = make(map[string]string)
	for _, p := range pods {
		port, err := randomAvailablePort()
		if err != nil {
			return err
		}
		portForwardCmd := exec.Command(k8sCli, "port-forward", "-n", p.Namespace, fmt.Sprintf("pod/%s", p.Name), fmt.Sprintf("%d:%d", port, metricsPort)) //nolint:gosec // perfectly safe to start port-forwarder for provided controller name
		logger.V(1).Info("starting port-forward", "command", strings.Join(portForwardCmd.Args, " "))
		if err := portForwardCmd.Start(); err != nil {
			logger.Error(err, fmt.Sprintf("failed to start port-forward for pod %s", p.Name))
			return err
		}
		waitFor(ctx, func() bool {
			resp, err := httpGet(fmt.Sprintf("https://localhost:%d%s", port, endpoint), token)
			if err != nil {
				return false
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				b, err := io.ReadAll(resp.Body)
				if err != nil {
					return false
				}
				sc.metricsResponse[p.Name] = string(b)
				return true
			}
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				return false
			}
			logger.V(1).Info("failed to get metrics", "pod", p.Name, "response", string(b))
			return false
		})
		if err := portForwardCmd.Process.Kill(); err != nil {
			return err
		}
		if _, err := portForwardCmd.Process.Wait(); err != nil {
			return err
		}
	}

	return nil
}

func CatalogIsUpdatedToVersion(name, version string) error {
	ref, err := k8sClient("get", "clustercatalog", fmt.Sprintf("%s-catalog", name), "-o", "jsonpath={.spec.source.image.ref}")
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
	_, err = k8sClient("patch", "clustercatalog", fmt.Sprintf("%s-catalog", name), "--type", "merge", "-p", string(pb))
	return err
}

func CatalogServesBundles(ctx context.Context, catalogName string) error {
	yamlContent, err := os.ReadFile(filepath.Join("steps", "testdata", fmt.Sprintf("%s-catalog-template.yaml", catalogName)))
	if err != nil {
		return fmt.Errorf("failed to read catalog yaml: %v", err)
	}

	_, err = k8scliWithInput(substituteScenarioVars(string(yamlContent), scenarioCtx(ctx)), "apply", "-f", "-")
	if err != nil {
		return fmt.Errorf("failed to apply catalog: %v", err)
	}

	return nil
}

func TagCatalogImage(name, oldTag, newTag string) error {
	imageRef := fmt.Sprintf("%s/%s", os.Getenv("LOCAL_REGISTRY_HOST"), fmt.Sprintf("e2e/%s-catalog:%s", name, oldTag))
	return crane.Tag(imageRef, newTag, crane.Insecure)
}

func CatalogIsDeleted(ctx context.Context, catalogName string) error {
	catalogFullName := fmt.Sprintf("%s-catalog", catalogName)
	_, err := k8sClient("delete", "clustercatalog", catalogFullName, "--ignore-not-found=true", "--wait=true")
	if err != nil {
		return fmt.Errorf("failed to delete catalog: %v", err)
	}
	return nil
}

func PrometheusMetricsAreReturned(ctx context.Context) error {
	sc := scenarioCtx(ctx)
	for podName, mr := range sc.metricsResponse {
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

func OperatorTargetNamespace(ctx context.Context, operator, namespace string) error {
	sc := scenarioCtx(ctx)
	namespace = substituteScenarioVars(namespace, sc)
	raw, err := k8sClient("get", "deployment", "-n", sc.namespace, operator, "-o", "json")
	if err != nil {
		return err
	}
	d := &appsv1.Deployment{}
	if err := json.Unmarshal([]byte(raw), d); err != nil {
		return err
	}

	if tns := d.Spec.Template.Annotations["olm.targetNamespaces"]; tns != namespace {
		return fmt.Errorf("expected target namespace %s, got %s", namespace, tns)
	}
	return nil
}

func MarkTestOperatorNotReady(ctx context.Context, state string) error {
	sc := scenarioCtx(ctx)
	v, err := k8sClient("get", "deployment", "-n", sc.namespace, "test-operator", "-o", "jsonpath={.spec.selector.matchLabels}")
	if err != nil {
		return err
	}
	var labels map[string]string
	if err := json.Unmarshal([]byte(v), &labels); err != nil {
		return err
	}
	podNameCmd := []string{"get", "pod", "-n", sc.namespace, "-o", "jsonpath={.items[0].metadata.name}"}
	for k, v := range labels {
		podNameCmd = append(podNameCmd, fmt.Sprintf("--selector=%s=%s", k, v))
	}
	podName, err := k8sClient(podNameCmd...)
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
	_, err = k8sClient("exec", podName, "-n", sc.namespace, "--", op, "/var/www/ready")
	return err
}

// SetCRDFieldMinValue patches a CRD to set the minimum value for a field.
// jsonPath is in the format ".spec.fieldName" and gets converted to the CRD schema path.
func SetCRDFieldMinValue(_ context.Context, resourceType, jsonPath string, minValue int) error {
	var crdName string
	switch resourceType {
	case "ClusterExtension":
		crdName = "clusterextensions.olm.operatorframework.io"
	case "ClusterExtensionRevision":
		crdName = "clusterextensionrevisions.olm.operatorframework.io"
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
	_, err := k8sClient("patch", "crd", crdName, "--type=json", "-p", patch)
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
		return ""
	}

	// Replace template variables
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

func getResource(kind string, name string, namespace string) (*unstructured.Unstructured, error) {
	out, err := k8sClient("get", kind, name, "-n", namespace, "-o", "yaml")
	if err != nil {
		return nil, err
	}
	obj, err := toUnstructured(out)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

// listExtensionResources returns a slice of client.Object containing all resources for a ClusterExtension
// this method is best called when the extension has been installed successfully. An error is returned if there was
// any issue in determining the extension's resources.
func listExtensionResources(extName string) ([]client.Object, error) {
	if enabled, found := featureGates[features.BoxcutterRuntime]; found && enabled {
		return listExtensionRevisionResources(extName)
	}
	return listHelmReleaseResources(extName)
}

// listHelmReleaseResources returns a slice of client.Object containing all resources for a ClusterExtension's
// Helm release. Note: The current implementation does not support release secrets chunked across multiple secrets
func listHelmReleaseResources(extName string) ([]client.Object, error) {
	secret, err := helmReleaseSecretForExtension(extName)
	if err != nil {
		return nil, fmt.Errorf("failed to get helm release secret for extension %s: %w", extName, err)
	}

	rel, err := helmReleaseFromSecret(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to get helm release from secret for cluster extension '%s': %w", extName, err)
	}

	objs, err := collectHelmReleaseObjects(rel)
	if err != nil {
		return nil, fmt.Errorf("failed to collect helm release objects for cluster extension '%s': %w", extName, err)
	}
	return objs, nil
}

// helmReleaseSecretForExtension returns the Helm release secret for the extension with name extName
func helmReleaseSecretForExtension(extName string) (*corev1.Secret, error) {
	out, err := k8sClient("get", "secrets", "-n", olmNamespace,
		"-l", fmt.Sprintf("name=%s,status=deployed", extName),
		"--field-selector", "type=operatorframework.io/index.v1", "-o", "json")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, err
	}

	var secretList corev1.SecretList
	if err = json.Unmarshal([]byte(out), &secretList); err != nil {
		return nil, err
	}
	if len(secretList.Items) != 1 {
		return nil, err
	}
	return &secretList.Items[0], nil
}

// helmReleaseFromSecret returns the Helm Release object encoded in the secret. Note: this function does not yet support
// releases chunked over multiple Secrets
func helmReleaseFromSecret(secret *corev1.Secret) (*release.Release, error) {
	// OLM uses a custom release backend that compresses the release data
	gzReader, err := gzip.NewReader(strings.NewReader(string(secret.Data["chunk"])))
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()

	releaseJsonBytes, err := io.ReadAll(gzReader)
	if err != nil {
		return nil, err
	}

	var rel release.Release
	if err = json.Unmarshal(releaseJsonBytes, &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// collectHelmReleaseObjects returns a slice of client.Object containing the manifests in rel
func collectHelmReleaseObjects(rel *release.Release) ([]client.Object, error) {
	result := k8sresource.NewLocalBuilder().Flatten().Unstructured().Stream(strings.NewReader(rel.Manifest), rel.Name).Do()
	if err := result.Err(); err != nil {
		return nil, err
	}
	infos, err := result.Infos()
	if err != nil {
		return nil, err
	}

	objs := make([]client.Object, 0, len(infos))
	for _, info := range infos {
		clientObject, ok := info.Object.(client.Object)
		if !ok {
			return nil, fmt.Errorf("object of type %T does not implement client.Object", info.Object)
		}
		objs = append(objs, clientObject)
	}
	return objs, nil
}

// listExtensionRevisionResources lists objects in the phases of the latest active revision
func listExtensionRevisionResources(extName string) ([]client.Object, error) {
	rev, err := latestActiveRevisionForExtension(extName)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest active revision for extension %s: %w", extName, err)
	}

	var objs []client.Object
	for i := range rev.Spec.Phases {
		phase := &rev.Spec.Phases[i]
		for j := range phase.Objects {
			objs = append(objs, &phase.Objects[j].Object)
		}
	}

	return objs, nil
}

// latestActiveRevisionForExtension returns the latest active revision for the extension called extName
func latestActiveRevisionForExtension(extName string) (*ocv1.ClusterExtensionRevision, error) {
	out, err := k8sClient("get", "clusterextensionrevisions", "-l", fmt.Sprintf("olm.operatorframework.io/owner-name=%s", extName), "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("error listing revisions for extension '%s': %w", extName, err)
	}
	if strings.TrimSpace(out) == "" {
		return nil, fmt.Errorf("no revisions found for extension '%s'", extName)
	}
	var revisionList ocv1.ClusterExtensionRevisionList
	if err := json.Unmarshal([]byte(out), &revisionList); err != nil {
		return nil, fmt.Errorf("error unmarshalling revisions for extension '%s': %w", extName, err)
	}

	var latest *ocv1.ClusterExtensionRevision
	for i := range revisionList.Items {
		rev := &revisionList.Items[i]
		if rev.Spec.LifecycleState != ocv1.ClusterExtensionRevisionLifecycleStateActive {
			continue
		}
		if latest == nil || rev.Spec.Revision > latest.Spec.Revision {
			latest = rev
		}
	}

	if latest == nil {
		return nil, fmt.Errorf("no active revisions found for extension '%s'", extName)
	}

	return latest, nil
}

// ResourceHasNoProxyEnvVars verifies that a deployment resource does NOT have proxy env vars
func ResourceHasNoProxyEnvVars(ctx context.Context, resource string) error {
	sc := scenarioCtx(ctx)
	resource = substituteScenarioVars(resource, sc)
	rtype, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <type>/<name>", resource)
	}

	if rtype != "deployment" {
		return fmt.Errorf("resource type %s is not supported for proxy env var checking, only deployment is supported", rtype)
	}

	// Use longer timeout for deployment updates (boxcutter can take >10 minutes)
	waitForWithTimeout(ctx, func() bool {
		raw, err := k8sClient("get", rtype, name, "-n", sc.namespace, "-o", "json")
		if err != nil {
			return false
		}

		// IMPORTANT: Create fresh struct on each iteration so old data doesn't persist
		var dep appsv1.Deployment
		if err := json.Unmarshal([]byte(raw), &dep); err != nil {
			return false
		}

		// Check that NO containers have proxy env vars
		for _, container := range dep.Spec.Template.Spec.Containers {
			for _, env := range container.Env {
				if env.Name == "HTTP_PROXY" || env.Name == "HTTPS_PROXY" || env.Name == "NO_PROXY" {
					return false
				}
			}
		}
		// Also check that NO init containers have proxy env vars
		for _, container := range dep.Spec.Template.Spec.InitContainers {
			for _, env := range container.Env {
				if env.Name == "HTTP_PROXY" || env.Name == "HTTPS_PROXY" || env.Name == "NO_PROXY" {
					return false
				}
			}
		}
		return true
	}, deploymentUpdateTimeout)

	return nil
}

// OperatorControllerHasEnvVar patches the operator-controller deployment to add an environment variable
func OperatorControllerHasEnvVar(ctx context.Context, envName, envValue string) error {
	// Get the current deployment first
	raw, err := k8sClient("get", "deployment", olmDeploymentName, "-n", olmNamespace, "-o", "json")
	if err != nil {
		return fmt.Errorf("failed to get operator-controller deployment: %w", err)
	}
	var dep appsv1.Deployment
	if err := json.Unmarshal([]byte(raw), &dep); err != nil {
		return fmt.Errorf("failed to unmarshal deployment: %w", err)
	}

	// Check if the env var already exists
	if len(dep.Spec.Template.Spec.Containers) > 0 {
		for _, env := range dep.Spec.Template.Spec.Containers[0].Env {
			if env.Name == envName {
				if env.Value == envValue {
					// Already has the correct value, no need to patch
					return nil
				}
				// Env var exists with different value, need to update
				break
			}
		}
	}

	// Use strategic merge patch to add/update the env var
	patchObj := map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []map[string]interface{}{
						{
							"name": "manager",
							"env": []map[string]interface{}{
								{
									"name":  envName,
									"value": envValue,
								},
							},
						},
					},
				},
			},
		},
	}
	patchBytes, err := json.Marshal(patchObj)
	if err != nil {
		return fmt.Errorf("failed to marshal deployment patch: %w", err)
	}
	patch := string(patchBytes)
	_, err = k8sClient("patch", "deployment", olmDeploymentName, "-n", olmNamespace, "--type=strategic", "-p", patch)
	if err != nil {
		return fmt.Errorf("failed to patch operator-controller deployment: %w", err)
	}

	// Wait for the deployment to roll out with the new env var
	waitFor(ctx, func() bool {
		raw, err := k8sClient("get", "deployment", olmDeploymentName, "-n", olmNamespace, "-o", "json")
		if err != nil {
			return false
		}
		var dep appsv1.Deployment
		if err := json.Unmarshal([]byte(raw), &dep); err != nil {
			return false
		}

		// Check deployment is available
		available := false
		for _, cond := range dep.Status.Conditions {
			if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue {
				available = true
				break
			}
		}
		if !available {
			return false
		}

		// Check all replicas are updated
		if dep.Status.UpdatedReplicas != dep.Status.Replicas {
			return false
		}

		// Check the env var is present in the first container (manager)
		if len(dep.Spec.Template.Spec.Containers) == 0 {
			return false
		}
		for _, env := range dep.Spec.Template.Spec.Containers[0].Env {
			if env.Name == envName && env.Value == envValue {
				return true
			}
		}
		return false
	})

	// Wait for operator-controller to fully start up with the new configuration
	waitForOperatorControllerStartup(ctx)

	return nil
}

// ResourceHasEnvVar verifies that a deployment resource has a specific environment variable with a specific value
func ResourceHasEnvVar(ctx context.Context, resource, envName, envValue string) error {
	sc := scenarioCtx(ctx)
	resource = substituteScenarioVars(resource, sc)
	rtype, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <type>/<name>", resource)
	}

	if rtype != "deployment" {
		return fmt.Errorf("resource type %s is not supported for env var checking, only deployment is supported", rtype)
	}

	waitFor(ctx, func() bool {
		var dep appsv1.Deployment
		raw, err := k8sClient("get", rtype, name, "-n", sc.namespace, "-o", "json")
		if err != nil {
			return false
		}
		if err := json.Unmarshal([]byte(raw), &dep); err != nil {
			return false
		}

		// Check that ALL containers have the env var with the correct value
		if len(dep.Spec.Template.Spec.Containers) == 0 {
			return false
		}
		for _, container := range dep.Spec.Template.Spec.Containers {
			found := false
			for _, env := range container.Env {
				if env.Name == envName && env.Value == envValue {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		// Also check that ALL init containers have the env var with the correct value
		for _, container := range dep.Spec.Template.Spec.InitContainers {
			found := false
			for _, env := range container.Env {
				if env.Name == envName && env.Value == envValue {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	})

	return nil
}

// OperatorControllerEnvVarRemoved removes an environment variable from the operator-controller deployment
func OperatorControllerEnvVarRemoved(ctx context.Context, envName string) error {
	// Use kubectl set env to remove the environment variable by name
	// This is more stable than JSON patch with array indices, which can break if env var ordering changes
	_, err := k8sClient("set", "env", "deployment/"+olmDeploymentName, "-n", olmNamespace, envName+"-")
	if err != nil {
		return fmt.Errorf("failed to remove env var %s from operator-controller deployment: %w", envName, err)
	}

	// Wait for the deployment to roll out without the env var
	waitFor(ctx, func() bool {
		raw, err := k8sClient("get", "deployment", olmDeploymentName, "-n", olmNamespace, "-o", "json")
		if err != nil {
			return false
		}
		var dep appsv1.Deployment
		if err := json.Unmarshal([]byte(raw), &dep); err != nil {
			return false
		}

		// Check deployment is available
		available := false
		for _, cond := range dep.Status.Conditions {
			if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue {
				available = true
				break
			}
		}
		if !available {
			return false
		}

		// Check all replicas are updated
		if dep.Status.UpdatedReplicas != dep.Status.Replicas {
			return false
		}

		// Check the env var is NOT present in the first container (manager)
		if len(dep.Spec.Template.Spec.Containers) == 0 {
			return false
		}
		for _, env := range dep.Spec.Template.Spec.Containers[0].Env {
			if env.Name == envName {
				return false
			}
		}
		return true
	})

	// Wait for operator-controller to fully start up with the new configuration
	waitForOperatorControllerStartup(ctx)
	logger.Info("operator-controller startup hook completed, proxy config applied")

	// Wait for reconciliation to complete - the ClusterExtension should reach rolled out state
	// with the new proxy configuration (or lack thereof)
	return ClusterExtensionIsRolledOut(ctx)
}

// ResourceHasNoEnvVar verifies that a deployment resource does NOT have a specific environment variable
func ResourceHasNoEnvVar(ctx context.Context, resource, envName string) error {
	sc := scenarioCtx(ctx)
	resource = substituteScenarioVars(resource, sc)
	rtype, name, found := strings.Cut(resource, "/")
	if !found {
		return fmt.Errorf("resource %s is not in the format <type>/<name>", resource)
	}

	if rtype != "deployment" {
		return fmt.Errorf("resource type %s is not supported for env var checking, only deployment is supported", rtype)
	}

	waitFor(ctx, func() bool {
		var dep appsv1.Deployment
		raw, err := k8sClient("get", rtype, name, "-n", sc.namespace, "-o", "json")
		if err != nil {
			return false
		}
		if err := json.Unmarshal([]byte(raw), &dep); err != nil {
			return false
		}

		// Check that NO containers have the env var
		for _, container := range dep.Spec.Template.Spec.Containers {
			for _, env := range container.Env {
				if env.Name == envName {
					return false
				}
			}
		}
		// Also check that NO init containers have the env var
		for _, container := range dep.Spec.Template.Spec.InitContainers {
			for _, env := range container.Env {
				if env.Name == envName {
					return false
				}
			}
		}
		return true
	})

	return nil
}

// ClusterExtensionRevisionHasNoProxyEnvVars verifies that the active ClusterExtensionRevision
// manifests do NOT contain proxy environment variables in deployment resources
// NOTE: This step only works with BoxcutterRuntime enabled. When BoxcutterRuntime is disabled,
// Rukpak/Helm is used instead, which doesn't create ClusterExtensionRevisions.
func ClusterExtensionRevisionHasNoProxyEnvVars(ctx context.Context) error {
	// Skip this check if BoxcutterRuntime is disabled
	// When using Rukpak/Helm, there are no ClusterExtensionRevisions to check
	if !featureGates[features.BoxcutterRuntime] {
		logger.Info("Skipping ClusterExtensionRevision manifest check - BoxcutterRuntime is disabled")
		return nil
	}

	sc := scenarioCtx(ctx)

	var revisionName string
	// Wait for active revision to be available - there might be a brief transition period
	// where the ClusterExtension is updating and activeRevisions is temporarily empty
	require.Eventually(godog.T(ctx), func() bool {
		raw, err := k8sClient("get", "clusterextension", sc.clusterExtensionName, "-o", "jsonpath={.status.activeRevisions[0].name}")
		if err != nil {
			logger.V(1).Info("failed to get active revision name", "error", err)
			return false
		}
		revisionName = strings.TrimSpace(raw)
		return revisionName != ""
	}, timeout, tick, "active revision not found after timeout")

	// Get the ClusterExtensionRevision
	revRaw, err := k8sClient("get", "clusterextensionrevision", revisionName, "-o", "json")
	if err != nil {
		return fmt.Errorf("failed to get revision %s: %w", revisionName, err)
	}

	var rev map[string]interface{}
	if err := json.Unmarshal([]byte(revRaw), &rev); err != nil {
		return fmt.Errorf("failed to unmarshal revision: %w", err)
	}

	// Check phases for deployment resources
	specVal, ok := rev["spec"]
	if !ok {
		return fmt.Errorf("revision %s has no spec field", revisionName)
	}

	specMap, ok := specVal.(map[string]interface{})
	if !ok {
		return fmt.Errorf("revision %s has unexpected spec structure (got %T)", revisionName, specVal)
	}

	phasesVal, found := specMap["phases"]
	if !found || phasesVal == nil {
		return fmt.Errorf("no phases found in revision %s", revisionName)
	}

	phases, ok := phasesVal.([]interface{})
	if !ok {
		return fmt.Errorf("revision %s has phases field with unexpected type (got %T)", revisionName, phasesVal)
	}

	for _, phase := range phases {
		phaseMap, ok := phase.(map[string]interface{})
		if !ok {
			return fmt.Errorf("revision %s has phase with unexpected structure (got %T)", revisionName, phase)
		}
		kindVal, ok := phaseMap["kind"].(string)
		if !ok || kindVal != "Deployment" {
			continue
		}

		// Get manifests from this phase
		manifestsVal, exists := phaseMap["manifests"]
		if !exists || manifestsVal == nil {
			continue
		}
		manifests, ok := manifestsVal.([]interface{})
		if !ok {
			return fmt.Errorf("revision %s has manifests field with unexpected type (got %T)", revisionName, manifestsVal)
		}

		for _, manifestData := range manifests {
			// manifestData is base64-encoded YAML
			manifestStr, ok := manifestData.(string)
			if !ok {
				return fmt.Errorf("unexpected manifest data type %T in revision %s", manifestData, revisionName)
			}

			// Decode base64
			decoded, err := base64.StdEncoding.DecodeString(manifestStr)
			if err != nil {
				return fmt.Errorf("failed to base64 decode manifest in revision %s: %w", revisionName, err)
			}

			// Check if the manifest contains proxy env vars
			manifestYaml := string(decoded)
			if strings.Contains(manifestYaml, "NO_PROXY") ||
				strings.Contains(manifestYaml, "HTTP_PROXY") ||
				strings.Contains(manifestYaml, "HTTPS_PROXY") {
				return fmt.Errorf("revision %s contains proxy environment variables in manifests", revisionName)
			}
		}
	}

	return nil
}
