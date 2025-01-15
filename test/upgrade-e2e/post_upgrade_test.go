package upgradee2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/utils/env"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	catalogd "github.com/operator-framework/operator-controller/catalogd/api/v1"
)

const (
	artifactName = "operator-controller-upgrade-e2e"
)

func TestClusterExtensionAfterOLMUpgrade(t *testing.T) {
	t.Log("Starting checks after OLM upgrade")
	ctx := context.Background()
	defer getArtifactsOutput(t)

	now := time.Now()

	// wait for catalogd deployment to finish
	t.Log("Wait for catalogd deployment to be ready")
	waitForDeployment(t, ctx, "catalogd-controller-manager")

	// wait for operator-controller deployment to finish
	t.Log("Wait for operator-controller deployment to be ready")
	waitForDeployment(t, ctx, "operator-controller-controller-manager")

	t.Log("Checking that the ClusterCatalog is unpacked")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var clusterCatalog catalogd.ClusterCatalog
		assert.NoError(ct, c.Get(ctx, types.NamespacedName{Name: testClusterCatalogName}, &clusterCatalog))

		// check serving condition
		cond := apimeta.FindStatusCondition(clusterCatalog.Status.Conditions, catalogd.TypeServing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, catalogd.ReasonAvailable, cond.Reason)

		// check progressing condition
		cond = apimeta.FindStatusCondition(clusterCatalog.Status.Conditions, catalogd.TypeProgressing)
		if !assert.NotNil(ct, cond) {
			return
		}
		assert.Equal(ct, metav1.ConditionTrue, cond.Status)
		assert.Equal(ct, catalogd.ReasonSucceeded, cond.Reason)

		// check that the catalog was recently unpacked (after progressing is over)
		t.Logf("last unpacked: %s - progressing last transitioned: %s", clusterCatalog.Status.LastUnpacked.String(), cond.LastTransitionTime.String())
		assert.True(ct, clusterCatalog.Status.LastUnpacked.After(now))
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
	}, time.Minute, time.Second)
}

func waitForDeployment(t *testing.T, ctx context.Context, controlPlaneLabel string) {
	deploymentLabelSelector := labels.Set{"control-plane": controlPlaneLabel}.AsSelector()

	t.Log("Checking that the deployment is updated")
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
	}, time.Minute, time.Second)

	var managerPods corev1.PodList
	t.Log("Waiting for only one Pod to remain")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, c.List(ctx, &managerPods, client.MatchingLabelsSelector{Selector: deploymentLabelSelector}))
		assert.Len(ct, managerPods.Items, 1)
	}, time.Minute, time.Second)
}

//func watchPodLogsForSubstring(ctx context.Context, pod *corev1.Pod, container string, substrings ...string) (bool, error) {
//	podLogOpts := corev1.PodLogOptions{
//		Follow:    true,
//		Container: container,
//	}
//
//	req := kclientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &podLogOpts)
//	podLogs, err := req.Stream(ctx)
//	if err != nil {
//		return false, err
//	}
//	defer podLogs.Close()
//
//	scanner := bufio.NewScanner(podLogs)
//	for scanner.Scan() {
//		line := scanner.Text()
//
//		foundCount := 0
//		for _, substring := range substrings {
//			if strings.Contains(line, substring) {
//				foundCount++
//			}
//		}
//		if foundCount == len(substrings) {
//			return true, nil
//		}
//	}
//
//	return false, scanner.Err()
//}

// getArtifactsOutput gets all the artifacts from the test run and saves them to the artifact path.
// Currently it saves:
// - clusterextensions
// - pods logs
// - deployments
// - catalogsources
func getArtifactsOutput(t *testing.T) {
	basePath := env.GetString("ARTIFACT_PATH", "")
	if basePath == "" {
		return
	}

	kubeClient, err := kubeclient.NewForConfig(cfg)
	require.NoError(t, err)

	// sanitize the artifact name for use as a directory name
	testName := strings.ReplaceAll(strings.ToLower(t.Name()), " ", "-")
	// Get the test description and sanitize it for use as a directory name
	artifactPath := filepath.Join(basePath, artifactName, fmt.Sprint(time.Now().UnixNano()), testName)

	// Create the full artifact path
	err = os.MkdirAll(artifactPath, 0755)
	require.NoError(t, err)

	// Get all namespaces
	namespaces := corev1.NamespaceList{}
	if err := c.List(context.Background(), &namespaces); err != nil {
		fmt.Printf("Failed to list namespaces: %v", err)
	}

	// get all cluster extensions save them to the artifact path.
	clusterExtensions := ocv1.ClusterExtensionList{}
	if err := c.List(context.Background(), &clusterExtensions, client.InNamespace("")); err != nil {
		fmt.Printf("Failed to list cluster extensions: %v", err)
	}
	for _, clusterExtension := range clusterExtensions.Items {
		// Save cluster extension to artifact path
		clusterExtensionYaml, err := yaml.Marshal(clusterExtension)
		if err != nil {
			fmt.Printf("Failed to marshal cluster extension: %v", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(artifactPath, clusterExtension.Name+"-clusterextension.yaml"), clusterExtensionYaml, 0600); err != nil {
			fmt.Printf("Failed to write cluster extension to file: %v", err)
		}
	}

	// get all catalogsources save them to the artifact path.
	catalogsources := catalogd.ClusterCatalogList{}
	if err := c.List(context.Background(), &catalogsources, client.InNamespace("")); err != nil {
		fmt.Printf("Failed to list catalogsources: %v", err)
	}
	for _, catalogsource := range catalogsources.Items {
		// Save catalogsource to artifact path
		catalogsourceYaml, err := yaml.Marshal(catalogsource)
		if err != nil {
			fmt.Printf("Failed to marshal catalogsource: %v", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(artifactPath, catalogsource.Name+"-catalogsource.yaml"), catalogsourceYaml, 0600); err != nil {
			fmt.Printf("Failed to write catalogsource to file: %v", err)
		}
	}

	for _, namespace := range namespaces.Items {
		// let's ignore kube-* namespaces.
		if strings.Contains(namespace.Name, "kube-") {
			continue
		}

		namespacedArtifactPath := filepath.Join(artifactPath, namespace.Name)
		if err := os.Mkdir(namespacedArtifactPath, 0755); err != nil {
			fmt.Printf("Failed to create namespaced artifact path: %v", err)
			continue
		}

		// get all deployments in the namespace and save them to the artifact path.
		deployments := appsv1.DeploymentList{}
		if err := c.List(context.Background(), &deployments, client.InNamespace(namespace.Name)); err != nil {
			fmt.Printf("Failed to list deployments %v in namespace: %q", err, namespace.Name)
			continue
		}

		for _, deployment := range deployments.Items {
			// Save deployment to artifact path
			deploymentYaml, err := yaml.Marshal(deployment)
			if err != nil {
				fmt.Printf("Failed to marshal deployment: %v", err)
				continue
			}
			if err := os.WriteFile(filepath.Join(namespacedArtifactPath, deployment.Name+"-deployment.yaml"), deploymentYaml, 0600); err != nil {
				fmt.Printf("Failed to write deployment to file: %v", err)
			}
		}

		// Get logs from all pods in all namespaces
		pods := corev1.PodList{}
		if err := c.List(context.Background(), &pods, client.InNamespace(namespace.Name)); err != nil {
			fmt.Printf("Failed to list pods %v in namespace: %q", err, namespace.Name)
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning && pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
				continue
			}
			for _, container := range pod.Spec.Containers {
				logs, err := kubeClient.CoreV1().Pods(namespace.Name).GetLogs(pod.Name, &corev1.PodLogOptions{Container: container.Name}).Stream(context.Background())
				if err != nil {
					fmt.Printf("Failed to get logs for pod %q in namespace %q: %v", pod.Name, namespace.Name, err)
					continue
				}
				defer logs.Close()

				outFile, err := os.Create(filepath.Join(namespacedArtifactPath, pod.Name+"-"+container.Name+"-logs.txt"))
				if err != nil {
					fmt.Printf("Failed to create file for pod %q in namespace %q: %v", pod.Name, namespace.Name, err)
					continue
				}
				defer outFile.Close()

				if _, err := io.Copy(outFile, logs); err != nil {
					fmt.Printf("Failed to copy logs for pod %q in namespace %q: %v", pod.Name, namespace.Name, err)
					continue
				}
			}
		}
	}
}
