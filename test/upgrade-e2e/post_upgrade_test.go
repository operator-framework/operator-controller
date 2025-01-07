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

	catalogd "github.com/operator-framework/catalogd/api/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestClusterExtensionAfterOLMUpgrade(t *testing.T) {
	t.Log("Starting checks after OLM upgrade")
	ctx := context.Background()

	managerLabelSelector := labels.Set{"control-plane": "operator-controller-controller-manager"}

	t.Log("Checking that the controller-manager deployment is updated")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var managerDeployments appsv1.DeploymentList
		assert.NoError(ct, c.List(ctx, &managerDeployments, client.MatchingLabelsSelector{Selector: managerLabelSelector.AsSelector()}))
		assert.Len(ct, managerDeployments.Items, 1)
		managerDeployment := managerDeployments.Items[0]

		assert.True(ct,
			managerDeployment.Status.UpdatedReplicas == *managerDeployment.Spec.Replicas &&
				managerDeployment.Status.Replicas == *managerDeployment.Spec.Replicas &&
				managerDeployment.Status.AvailableReplicas == *managerDeployment.Spec.Replicas &&
				managerDeployment.Status.ReadyReplicas == *managerDeployment.Spec.Replicas,
		)
	}, time.Minute, time.Second)

	var managerPods corev1.PodList
	t.Log("Waiting for only one controller-manager Pod to remain")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.List(ctx, &managerPods, client.MatchingLabelsSelector{Selector: managerLabelSelector.AsSelector()}))
		assert.Len(ct, managerPods.Items, 1)
	}, time.Minute, time.Second)

	t.Log("Reading logs to make sure that ClusterExtension was reconciled by operator-controller before we update it")
	// Make sure that after we upgrade OLM itself we can still reconcile old objects without any changes
	logCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	substrings := []string{
		"reconcile ending",
		fmt.Sprintf(`ClusterExtension=%q`, testClusterExtensionName),
	}
	found, err := watchPodLogsForSubstring(logCtx, &managerPods.Items[0], "manager", substrings...)
	require.NoError(t, err)
	require.True(t, found)

	t.Log("Checking that the ClusterCatalog is serving")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var clusterCatalog catalogd.ClusterCatalog
		assert.NoError(ct, c.Get(ctx, types.NamespacedName{Name: testClusterCatalogName}, &clusterCatalog))
		cond := apimeta.FindStatusCondition(clusterCatalog.Status.Conditions, catalogd.TypeServing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, catalogd.ReasonAvailable, cond.Reason)
	}, time.Minute, time.Second)

	t.Log("Checking that the ClusterExtension is installed")
	var clusterExtension ocv1.ClusterExtension
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.Get(ctx, types.NamespacedName{Name: testClusterExtensionName}, &clusterExtension))
		cond := apimeta.FindStatusCondition(clusterExtension.Status.Conditions, ocv1.TypeInstalled)
		if !assert.NotNil(ct, cond) {
			return
		}
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
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, ocv1.ReasonSucceeded, cond.Reason)
		assert.Contains(ct, cond.Message, "Installed bundle")
		assert.Equal(ct, ocv1.BundleMetadata{Name: "test-operator.1.0.1", Version: "1.0.1"}, clusterExtension.Status.Install.Bundle)
		assert.NotEqual(ct, previousVersion, clusterExtension.Status.Install.Bundle.Version)
	}, 3*time.Minute, time.Second)
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
