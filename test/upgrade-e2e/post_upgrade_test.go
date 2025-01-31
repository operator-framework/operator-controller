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
	catalogd "github.com/operator-framework/operator-controller/catalogd/api/v1"
	"github.com/operator-framework/operator-controller/test/utils"
)

const (
	artifactName = "operator-controller-upgrade-e2e"
)

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
	leaderElected, err := watchPodLogsForSubstring(leaderCtx, managerPod, "manager", leaderSubstrings...)
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
	found, err := watchPodLogsForSubstring(logCtx, managerPod, "manager", substrings...)
	require.NoError(t, err)
	require.True(t, found)

	t.Log("Checking that the ClusterCatalog is unpacked")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var clusterCatalog catalogd.ClusterCatalog
		assert.NoError(ct, c.Get(ctx, types.NamespacedName{Name: testClusterCatalogName}, &clusterCatalog))

		// check serving condition
		cond := apimeta.FindStatusCondition(clusterCatalog.Status.Conditions, catalogd.TypeServing)
		assert.NotNil(ct, cond)
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, catalogd.ReasonAvailable, cond.Reason)

		// mitigation for upgrade-e2e flakiness caused by the following bug
		// https://github.com/operator-framework/operator-controller/issues/1626
		// wait until the unpack time > than the catalogd controller pod creation time
		cond = apimeta.FindStatusCondition(clusterCatalog.Status.Conditions, catalogd.TypeProgressing)
		if cond == nil {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, catalogd.ReasonSucceeded, cond.Reason)

		assert.True(ct, clusterCatalog.Status.LastUnpacked.After(catalogdManagerPod.CreationTimestamp.Time))
	}, time.Minute, time.Second)

	t.Log("Checking that the ClusterExtension is installed")
	var clusterExtension ocv1.ClusterExtension
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(ctx, types.NamespacedName{Name: testClusterExtensionName}, &clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		assert.NotNil(ct, cond)
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		assert.Contains(ct, cond.Message, "Installed bundle")
		if assert.NotNil(ct, clusterExtension.Status.Install) {
			assert.NotEmpty(ct, clusterExtension.Status.Install.Bundle.Version)
		}
	}, time.Minute, time.Second)

	previousVersion := clusterExtension.Status.Install.Bundle.Version

	t.Log("Updating the ClusterExtension to change version")
	// Make sure that after we upgrade OLM itself we can still reconcile old objects if we change them
	clusterExtension.Spec.Source.Catalog.Version = "1.0.1"
	require.NoError(t, c.Update(ctx, &clusterExtension))

	t.Log("Checking that the ClusterExtension installs successfully")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(ctx, types.NamespacedName{Name: testClusterExtensionName}, &clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		assert.NotNil(ct, cond)
		assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		assert.Contains(ct, cond.Message, "Installed bundle")
		assert.Equal(ct, ocv1.BundleMetadata{Name: "test-operator.1.0.1", Version: "1.0.1"}, clusterExtension.Status.Install.Bundle)
		assert.NotEqual(ct, previousVersion, clusterExtension.Status.Install.Bundle.Version)
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
		assert.NoError(ct, c.List(ctx, &managerDeployments, client.MatchingLabelsSelector{Selector: deploymentLabelSelector}))
		assert.Len(ct, managerDeployments.Items, 1)
		managerDeployment := managerDeployments.Items[0]

		assert.True(ct,
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
		assert.NoError(ct, c.List(ctx, &managerPods, client.MatchingLabelsSelector{Selector: deploymentLabelSelector}))
		assert.Len(ct, managerPods.Items, 1)
	}, time.Minute, time.Second)
	return &managerPods.Items[0]
}

func watchPodLogsForSubstring(ctx context.Context, pod *corev1.Pod, container string, substrings ...string) (bool, error) {
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
