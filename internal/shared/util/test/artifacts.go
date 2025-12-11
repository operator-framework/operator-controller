package test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/env"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

// CollectTestArtifacts gets all the artifacts from the test run and saves them to the artifact path.
// Currently, it saves:
// - clusterextensions
// - clusterextensionrevisions
// - pods logs
// - deployments
// - catalogsources
func CollectTestArtifacts(t *testing.T, artifactName string, c client.Client, cfg *rest.Config) {
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
	if err := c.List(context.Background(), &clusterExtensions); err != nil {
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

	// get all cluster extension revisions save them to the artifact path.
	clusterExtensionRevisions := ocv1.ClusterExtensionRevisionList{}
	if err := c.List(context.Background(), &clusterExtensionRevisions); err != nil {
		fmt.Printf("Failed to list cluster extensions: %v", err)
	}
	for _, cer := range clusterExtensionRevisions.Items {
		// Save cluster extension to artifact path
		clusterExtensionYaml, err := yaml.Marshal(cer)
		if err != nil {
			fmt.Printf("Failed to marshal cluster extension: %v", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(artifactPath, cer.Name+"-clusterextensionrevision.yaml"), clusterExtensionYaml, 0600); err != nil {
			fmt.Printf("Failed to write cluster extension to file: %v", err)
		}
	}

	// get all catalogsources save them to the artifact path.
	catalogsources := ocv1.ClusterCatalogList{}
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

		// Get secrets in all namespaces
		secrets := corev1.SecretList{}
		if err := c.List(context.Background(), &secrets, client.InNamespace(namespace.Name)); err != nil {
			fmt.Printf("Failed to list secrets %v in namespace: %q", err, namespace.Name)
		}
		for _, secret := range secrets.Items {
			// Save secret to artifact path
			secretYaml, err := yaml.Marshal(secret)
			if err != nil {
				fmt.Printf("Failed to marshal secret: %v", err)
				continue
			}
			if err := os.WriteFile(filepath.Join(namespacedArtifactPath, secret.Name+"-secret.yaml"), secretYaml, 0600); err != nil {
				fmt.Printf("Failed to write secret to file: %v", err)
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
