package upgradee2e

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/test/utils"
)

const (
	artifactName = "operator-controller-upgrade-e2e"
	container    = "manager"
)

func TestClusterCatalogUnpacking(t *testing.T) {
	ctx := context.Background()

	t.Log("Checking that the controller-manager deployment is updated")
	managerLabelSelector := labels.Set{"control-plane": "catalogd-controller-manager"}
	var managerDeployment appsv1.Deployment
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var managerDeployments appsv1.DeploymentList
		err := c.List(ctx, &managerDeployments, client.MatchingLabels(managerLabelSelector), client.InNamespace("olmv1-system"))
		require.NoError(ct, err)
		require.Len(ct, managerDeployments.Items, 1)
		managerDeployment = managerDeployments.Items[0]
		require.Equal(ct, *managerDeployment.Spec.Replicas, managerDeployment.Status.UpdatedReplicas)
		require.Equal(ct, *managerDeployment.Spec.Replicas, managerDeployment.Status.Replicas)
		require.Equal(ct, *managerDeployment.Spec.Replicas, managerDeployment.Status.AvailableReplicas)
		require.Equal(ct, *managerDeployment.Spec.Replicas, managerDeployment.Status.ReadyReplicas)
	}, time.Minute, time.Second)

	var managerPod corev1.Pod
	t.Log("Waiting for only one controller-manager pod to remain")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var managerPods corev1.PodList
		err := c.List(ctx, &managerPods, client.MatchingLabels(managerLabelSelector))
		require.NoError(ct, err)
		require.Len(ct, managerPods.Items, 1)
		managerPod = managerPods.Items[0]
	}, time.Minute, time.Second)

	t.Log("Waiting for acquired leader election")
	leaderCtx, leaderCancel := context.WithTimeout(ctx, 3*time.Minute)
	defer leaderCancel()
	leaderSubstrings := []string{"successfully acquired lease"}
	leaderElected, err := watchPodLogsForSubstring(leaderCtx, &managerPod, leaderSubstrings...)
	require.NoError(t, err)
	require.True(t, leaderElected)

	t.Log("Reading logs to make sure that ClusterCatalog was reconciled by catalogdv1")
	logCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	substrings := []string{
		"reconcile ending",
		fmt.Sprintf(`ClusterCatalog=%q`, testClusterCatalogName),
	}
	found, err := watchPodLogsForSubstring(logCtx, &managerPod, substrings...)
	require.NoError(t, err)
	require.True(t, found)

	catalog := &ocv1.ClusterCatalog{}
	t.Log("Ensuring ClusterCatalog has Status.Condition of Progressing with a status == True, reason == Succeeded")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		err := c.Get(ctx, types.NamespacedName{Name: testClusterCatalogName}, catalog)
		require.NoError(ct, err)
		cond := apimeta.FindStatusCondition(catalog.Status.Conditions, ocv1.TypeProgressing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
	}, time.Minute, time.Second)

	t.Log("Ensuring ClusterCatalog has Status.Condition of Serving with a status == True, reason == Available")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		err := c.Get(ctx, types.NamespacedName{Name: testClusterCatalogName}, catalog)
		require.NoError(ct, err)
		cond := apimeta.FindStatusCondition(catalog.Status.Conditions, ocv1.TypeServing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonAvailable, cond.Reason)
	}, time.Minute, time.Second)
}

func TestClusterExtensionAfterOLMUpgrade(t *testing.T) {
	t.Log("Starting checks after OLM upgrade")
	ctx := context.Background()
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)

	// wait for catalogd deployment to finish
	t.Log("Wait for catalogd deployment to be ready")
	catalogdManagerPod := waitForDeployment(t, ctx, "catalogd-controller-manager")

	// wait for operator-controller deployment to finish
	t.Log("Wait for operator-controller deployment to be ready")
	managerPod := waitForDeployment(t, ctx, "operator-controller-controller-manager")

	t.Log("Wait for acquired leader election")
	// Average case is under 1 minute but in the worst case: (previous leader crashed)
	// we could have LeaseDuration (137s) + RetryPeriod (26s) +/- 163s
	leaderCtx, leaderCancel := context.WithTimeout(ctx, 3*time.Minute)
	defer leaderCancel()

	leaderSubstrings := []string{"successfully acquired lease"}
	leaderElected, err := watchPodLogsForSubstring(leaderCtx, managerPod, leaderSubstrings...)
	require.NoError(t, err)
	require.True(t, leaderElected)

	t.Log("Reading logs to make sure that ClusterExtension was reconciled by operator-controller before we update it")
	// Make sure that after we upgrade OLM itself we can still reconcile old objects without any changes
	logCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	substrings := []string{
		"reconcile ending",
		fmt.Sprintf(`ClusterExtension=%q`, testClusterExtensionName),
	}
	found, err := watchPodLogsForSubstring(logCtx, managerPod, substrings...)
	require.NoError(t, err)
	require.True(t, found)

	t.Log("Checking that the ClusterCatalog is unpacked")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var clusterCatalog ocv1.ClusterCatalog
		require.NoError(ct, c.Get(ctx, types.NamespacedName{Name: testClusterCatalogName}, &clusterCatalog))

		// check serving condition
		cond := apimeta.FindStatusCondition(clusterCatalog.Status.Conditions, ocv1.TypeServing)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonAvailable, cond.Reason)

		// mitigation for upgrade-e2e flakiness caused by the following bug
		// https://github.com/operator-framework/operator-controller/issues/1626
		// wait until the unpack time > than the catalogd controller pod creation time
		cond = apimeta.FindStatusCondition(clusterCatalog.Status.Conditions, ocv1.TypeProgressing)
		if cond == nil {
			return
		}
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)

		require.True(ct, clusterCatalog.Status.LastUnpacked.After(catalogdManagerPod.CreationTimestamp.Time))
	}, time.Minute, time.Second)

	t.Log("Checking that the ClusterExtension is installed")
	var clusterExtension ocv1.ClusterExtension
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(ctx, types.NamespacedName{Name: testClusterExtensionName}, &clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(ct, cond)
		require.Equal(ct, metav1.ConditionTrue, cond.Status)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		require.Contains(ct, cond.Message, "Installed bundle")
		require.NotNil(ct, clusterExtension.Status.Install)
		require.NotEmpty(ct, clusterExtension.Status.Install.Bundle.Version)
	}, time.Minute, time.Second)

	previousVersion := clusterExtension.Status.Install.Bundle.Version

	t.Log("Updating the ClusterExtension to change version")
	// Make sure that after we upgrade OLM itself we can still reconcile old objects if we change them
	clusterExtension.Spec.Source.Catalog.Version = "1.0.1"
	require.NoError(t, c.Update(ctx, &clusterExtension))

	t.Log("Checking that the ClusterExtension installs successfully")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.Get(ctx, types.NamespacedName{Name: testClusterExtensionName}, &clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		require.NotNil(ct, cond)
		require.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		require.Contains(ct, cond.Message, "Installed bundle")
		require.Equal(ct, ocv1.BundleMetadata{Name: "test-operator.1.0.1", Version: "1.0.1"}, clusterExtension.Status.Install.Bundle)
		require.NotEqual(ct, previousVersion, clusterExtension.Status.Install.Bundle.Version)
	}, time.Minute, time.Second)
}

// waitForDeployment checks that the updated deployment with the given control-plane label
// has reached the desired number of replicas and that the number pods matches that number
// i.e. no old pods remain. It will return a pointer to the first pod. This is only necessary
// to facilitate the mitigation put in place for https://github.com/operator-framework/operator-controller/issues/1626
func waitForDeployment(t *testing.T, ctx context.Context, controlPlaneLabel string) *corev1.Pod {
	deploymentLabelSelector := labels.Set{"control-plane": controlPlaneLabel}.AsSelector()

	t.Log("Checking that the deployment is updated")
	var desiredNumReplicas int32
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var managerDeployments appsv1.DeploymentList
		require.NoError(ct, c.List(ctx, &managerDeployments, client.MatchingLabelsSelector{Selector: deploymentLabelSelector}))
		require.Len(ct, managerDeployments.Items, 1)
		managerDeployment := managerDeployments.Items[0]

		require.True(ct,
			managerDeployment.Status.UpdatedReplicas == *managerDeployment.Spec.Replicas &&
				managerDeployment.Status.Replicas == *managerDeployment.Spec.Replicas &&
				managerDeployment.Status.AvailableReplicas == *managerDeployment.Spec.Replicas &&
				managerDeployment.Status.ReadyReplicas == *managerDeployment.Spec.Replicas,
		)
		desiredNumReplicas = *managerDeployment.Spec.Replicas
	}, time.Minute, time.Second)

	var managerPods corev1.PodList
	t.Logf("Ensure the number of remaining pods equal the desired number of replicas (%d)", desiredNumReplicas)
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		require.NoError(ct, c.List(ctx, &managerPods, client.MatchingLabelsSelector{Selector: deploymentLabelSelector}))
		require.Len(ct, managerPods.Items, 1)
	}, time.Minute, time.Second)
	return &managerPods.Items[0]
}

func watchPodLogsForSubstring(ctx context.Context, pod *corev1.Pod, substrings ...string) (bool, error) {
	podLogOpts := corev1.PodLogOptions{
		Follow:    true,
		Container: container,
	}

	req := kclientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOpts)
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return false, err
	}
	defer podLogs.Close()

	scanner := bufio.NewScanner(podLogs)
	for scanner.Scan() {
		line := scanner.Text()

		foundCount := 0
		for _, substring := range substrings {
			if strings.Contains(line, substring) {
				foundCount++
			}
		}
		if foundCount == len(substrings) {
			return true, nil
		}
	}

	return false, scanner.Err()
}
