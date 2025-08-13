// Package e2e contains end-to-end tests to verify that the metrics endpoints
// for both components. Metrics are exported and accessible by authorized users through
// RBAC and ServiceAccount tokens.
//
// These tests perform the following steps:
// 1. Create a ClusterRoleBinding to grant necessary permissions for accessing metrics.
// 2. Generate a ServiceAccount token for authentication.
// 3. Deploy a curl pod to interact with the metrics endpoint.
// 4. Wait for the curl pod to become ready.
// 5. Execute a curl command from the pod to validate the metrics endpoint.
// 6. Clean up all resources created during the test, such as the ClusterRoleBinding and curl pod.
//
//nolint:gosec
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/rand"

	utils "github.com/operator-framework/operator-controller/internal/shared/util/testutils"
)

// TestOperatorControllerMetricsExportedEndpoint verifies that the metrics endpoint for the operator controller
func TestOperatorControllerMetricsExportedEndpoint(t *testing.T) {
	client := utils.FindK8sClient(t)
	curlNamespace := createRandomNamespace(t, client)
	componentNamespace := getComponentNamespace(t, client, "control-plane=operator-controller-controller-manager")
	metricsURL := fmt.Sprintf("https://operator-controller-service.%s.svc.cluster.local:8443/metrics", componentNamespace)

	config := NewMetricsTestConfig(
		client,
		curlNamespace,
		"operator-controller-metrics-reader",
		"operator-controller-metrics-binding",
		"operator-controller-metrics-reader",
		"oper-curl-metrics",
		metricsURL,
	)

	config.run(t)
}

// TestCatalogdMetricsExportedEndpoint verifies that the metrics endpoint for catalogd
func TestCatalogdMetricsExportedEndpoint(t *testing.T) {
	client := utils.FindK8sClient(t)
	curlNamespace := createRandomNamespace(t, client)
	componentNamespace := getComponentNamespace(t, client, "control-plane=catalogd-controller-manager")
	metricsURL := fmt.Sprintf("https://catalogd-service.%s.svc.cluster.local:7443/metrics", componentNamespace)

	config := NewMetricsTestConfig(
		client,
		curlNamespace,
		"catalogd-metrics-reader",
		"catalogd-metrics-binding",
		"catalogd-metrics-reader",
		"catalogd-curl-metrics",
		metricsURL,
	)

	config.run(t)
}

// MetricsTestConfig holds the necessary configurations for testing metrics endpoints.
type MetricsTestConfig struct {
	client         string
	namespace      string
	clusterRole    string
	clusterBinding string
	serviceAccount string
	curlPodName    string
	metricsURL     string
}

// NewMetricsTestConfig initializes a new MetricsTestConfig.
func NewMetricsTestConfig(client, namespace, clusterRole, clusterBinding, serviceAccount, curlPodName, metricsURL string) *MetricsTestConfig {
	return &MetricsTestConfig{
		client:         client,
		namespace:      namespace,
		clusterRole:    clusterRole,
		clusterBinding: clusterBinding,
		serviceAccount: serviceAccount,
		curlPodName:    curlPodName,
		metricsURL:     metricsURL,
	}
}

// run will execute all steps of those tests
func (c *MetricsTestConfig) run(t *testing.T) {
	defer c.cleanup(t)

	c.createMetricsClusterRoleBinding(t)
	token := c.getServiceAccountToken(t)
	c.createCurlMetricsPod(t)
	c.validate(t, token)
}

// createMetricsClusterRoleBinding to binding and expose the metrics
func (c *MetricsTestConfig) createMetricsClusterRoleBinding(t *testing.T) {
	t.Logf("Creating ClusterRoleBinding %s for %s in namespace %s", c.clusterBinding, c.serviceAccount, c.namespace)
	cmd := exec.Command(c.client, "create", "clusterrolebinding", c.clusterBinding,
		"--clusterrole="+c.clusterRole,
		"--serviceaccount="+c.namespace+":"+c.serviceAccount)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Error creating ClusterRoleBinding: %s", string(output))
}

// getServiceAccountToken return the token requires to have access to the metrics
func (c *MetricsTestConfig) getServiceAccountToken(t *testing.T) string {
	t.Logf("Creating ServiceAccount %q in namespace %q", c.serviceAccount, c.namespace)
	output, err := exec.Command(c.client, "create", "serviceaccount", c.serviceAccount, "--namespace="+c.namespace).CombinedOutput()
	require.NoError(t, err, "Error creating service account: %v", string(output))

	t.Logf("Generating ServiceAccount token for %q in namespace %q", c.serviceAccount, c.namespace)
	cmd := exec.Command(c.client, "create", "token", c.serviceAccount, "--namespace", c.namespace)
	tokenOutput, tokenCombinedOutput, err := stdoutAndCombined(cmd)
	require.NoError(t, err, "Error creating token: %s", string(tokenCombinedOutput))
	return string(bytes.TrimSpace(tokenOutput))
}

// createCurlMetricsPod creates the Pod with curl image to allow check if the metrics are working
func (c *MetricsTestConfig) createCurlMetricsPod(t *testing.T) {
	t.Logf("Creating curl pod (%s/%s) to validate the metrics endpoint", c.namespace, c.curlPodName)
	cmd := exec.Command(c.client, "run", c.curlPodName,
		"--image=curlimages/curl:8.15.0",
		"--namespace", c.namespace,
		"--restart=Never",
		"--overrides", `{
			"spec": {
				"terminationGradePeriodSeconds": 0,
				"containers": [{
					"name": "curl",
					"image": "curlimages/curl:8.15.0",
					"command": ["sh", "-c", "sleep 3600"],
					"securityContext": {
						"allowPrivilegeEscalation": false,
						"capabilities": {"drop": ["ALL"]},
						"runAsNonRoot": true,
						"runAsUser": 1000,
						"seccompProfile": {"type": "RuntimeDefault"}
					}
				}],
				"serviceAccountName": "`+c.serviceAccount+`"
			}
		}`)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Error creating curl pod: %s", string(output))
}

// validate verifies if is possible to access the metrics
func (c *MetricsTestConfig) validate(t *testing.T, token string) {
	t.Log("Waiting for the curl pod to be ready")
	waitCmd := exec.Command(c.client, "wait", "--for=condition=Ready", "pod", c.curlPodName, "--namespace", c.namespace, "--timeout=60s")
	waitOutput, waitErr := waitCmd.CombinedOutput()
	require.NoError(t, waitErr, "Error waiting for curl pod to be ready: %s", string(waitOutput))

	t.Log("Validating the metrics endpoint")
	curlCmd := exec.Command(c.client, "exec", c.curlPodName, "--namespace", c.namespace, "--",
		"curl", "-v", "-k", "-H", "Authorization: Bearer "+token, c.metricsURL)
	output, err := curlCmd.CombinedOutput()
	require.NoError(t, err, "Error calling metrics endpoint: %s", string(output))
	require.Contains(t, string(output), "200 OK", "Metrics endpoint did not return 200 OK")
}

// cleanup removes the created resources. Uses a context with timeout to prevent hangs.
func (c *MetricsTestConfig) cleanup(t *testing.T) {
	type objDesc struct {
		resourceName string
		name         string
		namespace    string
	}
	objects := []objDesc{
		{"clusterrolebinding", c.clusterBinding, ""},
		{"pod", c.curlPodName, c.namespace},
		{"serviceaccount", c.serviceAccount, c.namespace},
		{"namespace", c.namespace, ""},
	}

	t.Log("Cleaning up resources")
	for _, obj := range objects {
		args := []string{"delete", obj.resourceName, obj.name, "--ignore-not-found=true", "--force"}
		if obj.namespace != "" {
			args = append(args, "--namespace", obj.namespace)
		}
		output, err := exec.Command(c.client, args...).CombinedOutput()
		require.NoError(t, err, "Error deleting %q %q in namespace %q: %v", obj.resourceName, obj.name, obj.namespace, string(output))
	}

	// Create a context with a 60-second timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, obj := range objects {
		err := waitForDeletion(ctx, c.client, obj.resourceName, obj.name, obj.namespace)
		require.NoError(t, err, "Error deleting %q %q in namespace %q", obj.resourceName, obj.name, obj.namespace)
		t.Logf("Successfully deleted %q %q in namespace %q", obj.resourceName, obj.name, obj.namespace)
	}
}

// waitForDeletion uses "kubectl wait" to block until the specified resource is deleted
// or until the 60-second timeout is reached.
func waitForDeletion(ctx context.Context, client, resourceType, resourceName, resourceNamespace string) error {
	args := []string{"wait", "--for=delete", "--timeout=60s", resourceType, resourceName}
	if resourceNamespace != "" {
		args = append(args, "--namespace", resourceNamespace)
	}
	cmd := exec.CommandContext(ctx, client, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error waiting for deletion of %s %s: %v, output: %s", resourceType, resourceName, err, string(output))
	}
	return nil
}

// createRandomNamespace creates a random namespace
func createRandomNamespace(t *testing.T, client string) string {
	nsName := fmt.Sprintf("testns-%s", rand.String(8))

	cmd := exec.Command(client, "create", "namespace", nsName)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Error creating namespace: %s", string(output))

	return nsName
}

// getComponentNamespace returns the namespace where operator-controller or catalogd is running
func getComponentNamespace(t *testing.T, client, selector string) string {
	cmd := exec.Command(client, "get", "pods", "--all-namespaces", "--selector="+selector, "--output=jsonpath={.items[0].metadata.namespace}")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Error determining namespace: %s", string(output))

	namespace := string(bytes.TrimSpace(output))
	if namespace == "" {
		t.Fatal("No namespace found for selector " + selector)
	}
	return namespace
}

func stdoutAndCombined(cmd *exec.Cmd) ([]byte, []byte, error) {
	var outOnly, outAndErr bytes.Buffer
	allWriter := io.MultiWriter(&outOnly, &outAndErr)
	cmd.Stdout = allWriter
	cmd.Stderr = &outAndErr
	err := cmd.Run()
	return outOnly.Bytes(), outAndErr.Bytes(), err
}
