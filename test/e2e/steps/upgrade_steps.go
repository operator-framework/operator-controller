package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	catalogdv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

// waitForUpgradeCheck retains the last failed assertion, including controller
// conditions, so migration failures report their cause rather than just a timeout.
func waitForUpgradeCheck(ctx context.Context, check func() error) error {
	var lastErr error
	err := wait.PollUntilContextTimeout(ctx, tick, timeout, true, func(context.Context) (bool, error) {
		lastErr = check()
		return lastErr == nil, nil
	})
	if err != nil {
		return fmt.Errorf("upgrade assertion timed out: %w: %v", err, lastErr)
	}
	return nil
}

// leaseNames maps component labels to their leader election lease names.
var leaseNames = map[string]string{
	"catalogd":            "catalogd-operator-lock",
	"operator-controller": "9c4404e7.operatorframework.io",
}

// LatestStableOLMReleaseIsInstalled downloads and executes the latest stable OLM release install script.
func LatestStableOLMReleaseIsInstalled(_ context.Context) error {
	if err := runInstallScript("RELEASE_INSTALL"); err != nil {
		return err
	}
	if err := refreshOLMConfiguration(); err != nil {
		return err
	}
	if featureGates[features.BoxcutterRuntime] {
		return fmt.Errorf("upgrade coverage requires an initial standard OLM installation")
	}
	return nil
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
func OLMIsUpgraded(_ context.Context) error {
	if err := runInstallScript("RELEASE_UPGRADE"); err != nil {
		return err
	}
	return refreshOLMConfiguration()
}

// ComponentIsReadyToReconcile waits for the named component's deployment to be fully rolled out,
// then checks the leader election lease and stores the leader pod name in the scenario context.
func ComponentIsReadyToReconcile(ctx context.Context, component string) error {
	sc := scenarioCtx(ctx)
	ns := namespaceForComponent(component)

	// Wait for deployment rollout to complete
	depName, err := k8sClient(ctx, "get", "deployments", "-n", ns,
		"-l", fmt.Sprintf("app.kubernetes.io/name=%s", component),
		"-o", "jsonpath={.items[0].metadata.name}")
	if err != nil {
		return fmt.Errorf("failed to find deployment for component %s in namespace %s: %w", component, ns, err)
	}
	if depName == "" {
		return fmt.Errorf("failed to find deployment for component %s in namespace %s: no matching deployments found", component, ns)
	}
	if _, err := k8sClient(ctx, "rollout", "status", fmt.Sprintf("deployment/%s", depName),
		"-n", ns, fmt.Sprintf("--timeout=%s", timeout)); err != nil {
		return fmt.Errorf("deployment rollout failed for %s: %w", component, err)
	}

	// Check leader election lease
	leaseName, ok := leaseNames[component]
	if !ok {
		return fmt.Errorf("no lease name for component: %s", component)
	}

	// Leader election can take up to LeaseDuration (137s) + RetryPeriod (26s) ≈ 163s in the worst case
	if err := waitForUpgradeCheck(ctx, func() error {
		output, err := k8sClient(ctx, "get", "lease", leaseName, "-n", ns,
			"-o", "jsonpath={.spec.holderIdentity}")
		if err != nil || output == "" {
			return fmt.Errorf("waiting for %s leader lease: %s (%v)", component, output, err)
		}
		// Extract pod name from lease holder identity (format: <pod-name>_<suffix>)
		podName := strings.Split(output, "_")[0]
		// A lease left by the previous controller is insufficient. Require its
		// holder to belong to the fully rolled-out deployment's current ReplicaSet.
		podJSON, err := k8sClient(ctx, "get", "pod", podName, "-n", ns, "-o", "json")
		if err != nil {
			return fmt.Errorf("reading %s leader pod: %w", component, err)
		}
		var pod corev1.Pod
		if err := json.Unmarshal([]byte(podJSON), &pod); err != nil {
			return err
		}
		if !podReady(&pod) || metav1.GetControllerOf(&pod) == nil {
			return fmt.Errorf("%s leader pod %s is not ready or has no controller", component, podName)
		}
		rsJSON, err := k8sClient(ctx, "get", "replicaset", metav1.GetControllerOf(&pod).Name, "-n", ns, "-o", "json")
		if err != nil {
			return err
		}
		depJSON, err := k8sClient(ctx, "get", "deployment", depName, "-n", ns, "-o", "json")
		if err != nil {
			return err
		}
		var rs appsv1.ReplicaSet
		var dep appsv1.Deployment
		if err := json.Unmarshal([]byte(rsJSON), &rs); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(depJSON), &dep); err != nil {
			return err
		}
		owner := metav1.GetControllerOf(&rs)
		revision := dep.Annotations["deployment.kubernetes.io/revision"]
		if owner == nil || owner.Kind != "Deployment" || owner.UID != dep.UID ||
			metav1.GetControllerOf(&pod).Kind != "ReplicaSet" || metav1.GetControllerOf(&pod).UID != rs.UID ||
			!generationObserved(dep.Generation, dep.Status.ObservedGeneration) || revision == "" || rs.Annotations["deployment.kubernetes.io/revision"] != revision {
			return fmt.Errorf("%s lease holder %s is not from deployment revision %s", component, podName, revision)
		}
		sc.leaderPods[component] = podName
		return nil
	}); err != nil {
		return err
	}

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
func reconcileEndingCheck(ctx context.Context, namespace, leaderPod, resourceName string) func() bool {
	return func() bool {
		logs, err := k8sClient(ctx, "logs", leaderPod, "-n", namespace, "--all-containers=true", "--tail=1000")
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

// ClusterExtensionIsReconciled waits for the ClusterExtension to be reconciled
// by checking the leader pod's logs for a "reconcile ending" entry.
func ClusterExtensionIsReconciled(ctx context.Context) error {
	sc := scenarioCtx(ctx)

	component := "operator-controller"
	resourceName := sc.clusterExtensionName
	if resourceName == "" {
		return fmt.Errorf("no ClusterExtension found in scenario context")
	}

	if err := ComponentIsReadyToReconcile(ctx, component); err != nil {
		return err
	}

	leaderPod := sc.leaderPods[component]
	waitFor(ctx, reconcileEndingCheck(ctx, namespaceForComponent(component), leaderPod, resourceName))
	return nil
}

// clusterCatalogUnpackedAfterPodCreation returns a check function that verifies the
// ClusterCatalog is serving and its lastUnpacked timestamp is after the leader pod's creation.
func clusterCatalogUnpackedAfterPodCreation(ctx context.Context, namespace, resourceName, leaderPod string) func() bool {
	return func() bool {
		catalogJSON, err := k8sClient(ctx, "get", "clustercatalog", resourceName, "-o", "json")
		if err != nil {
			return false
		}
		var catalog catalogdv1.ClusterCatalog
		if err := json.Unmarshal([]byte(catalogJSON), &catalog); err != nil {
			return false
		}

		serving := currentCondition(catalog.Generation, catalog.Status.Conditions, catalogdv1.TypeServing)
		if serving == nil || serving.Status != metav1.ConditionTrue || serving.Reason != catalogdv1.ReasonAvailable {
			return false
		}
		progressing := currentCondition(catalog.Generation, catalog.Status.Conditions, catalogdv1.TypeProgressing)
		if progressing == nil || progressing.Status != metav1.ConditionTrue || progressing.Reason != catalogdv1.ReasonSucceeded {
			return false
		}

		if catalog.Status.LastUnpacked == nil {
			return false
		}

		podJSON, err := k8sClient(ctx, "get", "pod", leaderPod, "-n", namespace, "-o", "json")
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
	output, err := k8sClient(ctx, "get", pluralType, "-o", "jsonpath={.items[*].metadata.name}")
	if err != nil {
		return fmt.Errorf("failed to list %s resources: %w", resourceType, err)
	}
	resourceNames := strings.Fields(output)
	if len(resourceNames) == 0 {
		return fmt.Errorf("no %s resources found", resourceType)
	}

	ns := namespaceForComponent(component)
	for _, name := range resourceNames {
		waitFor(ctx, reconcileEndingCheck(ctx, ns, leaderPod, name))
	}

	return nil
}

// ScenarioCatalogIsReconciled waits for a named per-scenario ClusterCatalog to be reconciled by checking leader pod logs.
func ScenarioCatalogIsReconciled(ctx context.Context, catalogUserName string) error {
	sc := scenarioCtx(ctx)
	catalogName, ok := sc.catalogs[catalogUserName]
	if !ok {
		return fmt.Errorf("no catalog %q has been created for this scenario", catalogUserName)
	}

	component := "catalogd"
	if err := ComponentIsReadyToReconcile(ctx, component); err != nil {
		return err
	}

	leaderPod, ok := sc.leaderPods[component]
	if !ok {
		return fmt.Errorf("leader pod not found for component %s", component)
	}

	ns := namespaceForComponent(component)
	waitFor(ctx, reconcileEndingCheck(ctx, ns, leaderPod, catalogName))

	// Also verify that lastUnpacked is after the leader pod's creation.
	// This mitigates flakiness caused by https://github.com/operator-framework/operator-controller/issues/1626
	waitFor(ctx, clusterCatalogUnpackedAfterPodCreation(ctx, ns, catalogName, leaderPod))
	return nil
}

// ScenarioCatalogReportsCondition waits for a named per-scenario ClusterCatalog to have the specified condition.
func ScenarioCatalogReportsCondition(ctx context.Context, catalogUserName, conditionType, conditionStatus, conditionReason string) error {
	sc := scenarioCtx(ctx)
	catalogName, ok := sc.catalogs[catalogUserName]
	if !ok {
		if _, err := k8sClient(ctx, "get", "clustercatalog", catalogUserName); err != nil {
			return fmt.Errorf("catalog %q was not created by this scenario and does not exist on the cluster", catalogUserName)
		}
		catalogName = catalogUserName
	}
	err := waitForCondition(ctx, "clustercatalog", catalogName, conditionType, metav1.ConditionStatus(conditionStatus), &conditionReason, nil)
	if err == nil {
		if rec := RecorderFromContext(ctx); rec != nil {
			out, _ := k8sClient(ctx, "get", "clustercatalog", catalogName,
				"-o", fmt.Sprintf("jsonpath={.status.conditions[?(@.type==\"%s\")]}", conditionType))
			rec.RecordCustom(
				fmt.Sprintf("kubectl get clustercatalog %s -o jsonpath='{.status.conditions[?(@.type==\"%s\")]}' | jq .",
					catalogName, conditionType),
				out+"\n", "",
			)
		}
	}
	return err
}
