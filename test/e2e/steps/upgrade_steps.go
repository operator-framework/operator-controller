package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	catalogdv1 "github.com/operator-framework/operator-controller/api/v1"
)

// leaseNames maps component labels to their leader election lease names.
var leaseNames = map[string]string{
	"catalogd":            "catalogd-operator-lock",
	"operator-controller": "9c4404e7.operatorframework.io",
}

var (
	installOperator = sync.OnceValue(func() error {
		err := runInstallScript("RELEASE_INSTALL")
		if err != nil {
			return err
		}
		olm, err := detectOLMDeployment()
		if err != nil {
			return err
		}
		olmNamespace = olm.Namespace
		return nil
	})
	upgradeOperator = sync.OnceValue(func() error {
		return runInstallScript("RELEASE_UPGRADE")
	})
)

// LatestStableOLMReleaseIsInstalled downloads and executes the latest stable OLM release install script.
// Uses sync.Once to ensure the install only happens once across multiple scenarios.
func LatestStableOLMReleaseIsInstalled(_ context.Context) error {
	return installOperator()
}

func runInstallScript(envVar string) error {
	scriptPath, found := os.LookupEnv(envVar)
	if !found {
		return fmt.Errorf("missing %s env variable", envVar)
	}
	if scriptPath == "" {
		return fmt.Errorf("%s environment variable must contain install script location", envVar)
	}
	var cmd *exec.Cmd
	if u, err := url.Parse(scriptPath); err == nil && u.Scheme != "" {
		cmd = exec.Command("bash", "-c", fmt.Sprintf("curl -L -s %s | bash -s", scriptPath)) //nolint:gosec // scriptPath is from a trusted env variable
	} else {
		cmd = exec.Command("bash", scriptPath)
	}
	dir, _ := os.LookupEnv("ROOT_DIR")
	if dir == "" {
		return fmt.Errorf("ROOT_DIR environment variable not set")
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// OLMIsUpgraded applies the locally built OLM manifest to upgrade OLM.
// Uses sync.Once to ensure the upgrade only happens once across multiple scenarios.
func OLMIsUpgraded(ctx context.Context) error {
	return upgradeOperator()
}

// ComponentIsReadyToReconcile waits for the named component's deployment to be fully rolled out,
// then checks the leader election lease and stores the leader pod name in the scenario context.
func ComponentIsReadyToReconcile(ctx context.Context, component string) error {
	sc := scenarioCtx(ctx)

	// Wait for deployment rollout to complete
	depName, err := k8sClient("get", "deployments", "-n", olmNamespace,
		"-l", fmt.Sprintf("app.kubernetes.io/name=%s", component),
		"-o", "jsonpath={.items[0].metadata.name}")
	if err != nil {
		return fmt.Errorf("failed to find deployment for component %s: %w", component, err)
	}
	if depName == "" {
		return fmt.Errorf("failed to find deployment for component %s: no matching deployments found", component)
	}
	if _, err := k8sClient("rollout", "status", fmt.Sprintf("deployment/%s", depName),
		"-n", olmNamespace, fmt.Sprintf("--timeout=%s", timeout)); err != nil {
		return fmt.Errorf("deployment rollout failed for %s: %w", component, err)
	}

	// Check leader election lease
	leaseName, ok := leaseNames[component]
	if !ok {
		return fmt.Errorf("no lease name for component: %s", component)
	}

	// Leader election can take up to LeaseDuration (137s) + RetryPeriod (26s) ≈ 163s in the worst case
	waitFor(ctx, func() bool {
		output, err := k8sClient("get", "lease", leaseName, "-n", olmNamespace,
			"-o", "jsonpath={.spec.holderIdentity}")
		if err != nil || output == "" {
			return false
		}
		// Extract pod name from lease holder identity (format: <pod-name>_<suffix>)
		podName := strings.Split(output, "_")[0]
		sc.leaderPods[component] = podName
		return true
	})

	logger.V(1).Info("Component is ready to reconcile", "component", component, "leaderPod", sc.leaderPods[component])
	return nil
}

// resourceTypeToComponent maps resource type names to their controller component labels.
var resourceTypeToComponent = map[string]string{
	"ClusterCatalog":   "catalogd",
	"ClusterExtension": "operator-controller",
}

// reconcileEndingCheck returns a function that checks whether the leader pod's logs
// contain a "reconcile ending" entry for the given resource name.
func reconcileEndingCheck(leaderPod, resourceName string) func() bool {
	return func() bool {
		logs, err := k8sClient("logs", leaderPod, "-n", olmNamespace, "--all-containers=true", "--tail=1000")
		if err != nil {
			return false
		}
		for _, line := range strings.Split(logs, "\n") {
			if strings.Contains(line, "reconcile ending") && strings.Contains(line, resourceName) {
				return true
			}
		}
		return false
	}
}

// ResourceTypeIsReconciled waits for the component's deployment to be ready and
// then verifies that the scenario's resource of the given type has been reconciled
// by checking the leader pod's logs for a "reconcile ending" entry.
func ResourceTypeIsReconciled(ctx context.Context, resourceType string) error {
	sc := scenarioCtx(ctx)

	component, ok := resourceTypeToComponent[resourceType]
	if !ok {
		return fmt.Errorf("unknown resource type: %s", resourceType)
	}

	var resourceName string
	switch resourceType {
	case "ClusterCatalog":
		resourceName = sc.clusterCatalogName
	case "ClusterExtension":
		resourceName = sc.clusterExtensionName
	}
	if resourceName == "" {
		return fmt.Errorf("no %s found in scenario context", resourceType)
	}

	if err := ComponentIsReadyToReconcile(ctx, component); err != nil {
		return err
	}

	leaderPod := sc.leaderPods[component]
	waitFor(ctx, reconcileEndingCheck(leaderPod, resourceName))

	// For ClusterCatalog, also verify that lastUnpacked is after the leader pod's creation.
	// This mitigates flakiness caused by https://github.com/operator-framework/operator-controller/issues/1626
	if resourceType == "ClusterCatalog" {
		waitFor(ctx, clusterCatalogUnpackedAfterPodCreation(resourceName, leaderPod))
	}

	return nil
}

// clusterCatalogUnpackedAfterPodCreation returns a check function that verifies the
// ClusterCatalog is serving and its lastUnpacked timestamp is after the leader pod's creation.
func clusterCatalogUnpackedAfterPodCreation(resourceName, leaderPod string) func() bool {
	return func() bool {
		catalogJSON, err := k8sClient("get", "clustercatalog", resourceName, "-o", "json")
		if err != nil {
			return false
		}
		var catalog catalogdv1.ClusterCatalog
		if err := json.Unmarshal([]byte(catalogJSON), &catalog); err != nil {
			return false
		}

		serving := apimeta.FindStatusCondition(catalog.Status.Conditions, catalogdv1.TypeServing)
		if serving == nil || serving.Status != metav1.ConditionTrue || serving.Reason != catalogdv1.ReasonAvailable {
			return false
		}
		progressing := apimeta.FindStatusCondition(catalog.Status.Conditions, catalogdv1.TypeProgressing)
		if progressing == nil || progressing.Status != metav1.ConditionTrue || progressing.Reason != catalogdv1.ReasonSucceeded {
			return false
		}

		if catalog.Status.LastUnpacked == nil {
			return false
		}

		podJSON, err := k8sClient("get", "pod", leaderPod, "-n", olmNamespace, "-o", "json")
		if err != nil {
			return false
		}
		var pod corev1.Pod
		if err := json.Unmarshal([]byte(podJSON), &pod); err != nil {
			return false
		}

		return catalog.Status.LastUnpacked.After(pod.CreationTimestamp.Time)
	}
}

// allResourcesAreReconciled discovers all cluster-scoped resources of the given type and verifies
// that the leader pod's logs contain a "reconcile ending" entry for each resource.
func allResourcesAreReconciled(ctx context.Context, resourceType string) error {
	sc := scenarioCtx(ctx)

	component, ok := resourceTypeToComponent[resourceType]
	if !ok {
		return fmt.Errorf("unknown resource type: %s", resourceType)
	}

	leaderPod, ok := sc.leaderPods[component]
	if !ok {
		return fmt.Errorf("leader pod not found for component %s; run '%s is ready to reconcile resources' first", component, component)
	}

	// Discover all resources
	pluralType := strings.ToLower(resourceType) + "s"
	output, err := k8sClient("get", pluralType, "-o", "jsonpath={.items[*].metadata.name}")
	if err != nil {
		return fmt.Errorf("failed to list %s resources: %w", resourceType, err)
	}
	resourceNames := strings.Fields(output)
	if len(resourceNames) == 0 {
		return fmt.Errorf("no %s resources found", resourceType)
	}

	for _, name := range resourceNames {
		waitFor(ctx, reconcileEndingCheck(leaderPod, name))
	}

	return nil
}

// ClusterCatalogReportsCondition waits for the ClusterCatalog to have the specified condition type, status, and reason.
func ClusterCatalogReportsCondition(ctx context.Context, conditionType, conditionStatus, conditionReason string) error {
	sc := scenarioCtx(ctx)
	if sc.clusterCatalogName == "" {
		return fmt.Errorf("cluster catalog name not set; run 'ClusterCatalog serves bundles' first")
	}
	return waitForCondition(ctx, "clustercatalog", sc.clusterCatalogName, conditionType, conditionStatus, &conditionReason, nil)
}
