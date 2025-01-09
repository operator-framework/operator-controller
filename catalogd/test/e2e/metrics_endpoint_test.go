package e2e

import (
	"bytes"
	"io"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// nolint:gosec
// TestCatalogdMetricsExportedEndpoint verifies that the metrics endpoint for the catalogd
// is exported correctly and accessible by authorized users through RBAC and a ServiceAccount token.
// The test performs the following steps:
// 1. Creates a ClusterRoleBinding to grant necessary permissions for accessing metrics.
// 2. Generates a ServiceAccount token for authentication.
// 3. Deploys a curl pod to interact with the metrics endpoint.
// 4. Waits for the curl pod to become ready.
// 5. Executes a curl command from the pod to validate the metrics endpoint.
// 6. Cleans up all resources created during the test, such as the ClusterRoleBinding and curl pod.
func TestCatalogdMetricsExportedEndpoint(t *testing.T) {
	var (
		token   string
		curlPod = "curl-metrics"
		client  = ""
		clients = []string{"kubectl", "oc"}
	)

	t.Log("Looking for k8s client")
	for _, c := range clients {
		// Would prefer to use `command -v`, but even that may not be installed!
		err := exec.Command(c, "version", "--client").Run()
		if err == nil {
			client = c
			break
		}
	}
	if client == "" {
		t.Fatal("k8s client not found")
	}
	t.Logf("Using %q as k8s client", client)

	t.Log("Determining catalogd namespace")
	cmd := exec.Command(client, "get", "pods", "--all-namespaces", "--selector=control-plane=catalogd-controller-manager", "--output=jsonpath={.items[0].metadata.namespace}")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Error creating determining catalogd namespace: %s", string(output))
	namespace := string(output)
	if namespace == "" {
		t.Fatal("No catalogd namespace found")
	}
	t.Logf("Using %q as catalogd namespace", namespace)

	t.Log("Creating ClusterRoleBinding for metrics access")
	cmd = exec.Command(client, "create", "clusterrolebinding", "catalogd-metrics-binding",
		"--clusterrole=catalogd-metrics-reader",
		"--serviceaccount="+namespace+":catalogd-controller-manager")
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Error creating ClusterRoleBinding: %s", string(output))

	defer func() {
		t.Log("Cleaning up ClusterRoleBinding")
		_ = exec.Command(client, "delete", "clusterrolebinding", "catalogd-metrics-binding", "--ignore-not-found=true").Run()
	}()

	t.Log("Creating service account token for authentication")
	tokenCmd := exec.Command(client, "create", "token", "catalogd-controller-manager", "-n", namespace)
	tokenOutput, tokenCombinedOutput, err := stdoutAndCombined(tokenCmd)
	require.NoError(t, err, "Error creating token: %s", string(tokenCombinedOutput))
	token = string(bytes.TrimSpace(tokenOutput))

	t.Log("Creating a pod to run curl commands")
	cmd = exec.Command(client, "run", curlPod,
		"--image=curlimages/curl:7.87.0", "-n", namespace,
		"--restart=Never",
		"--overrides", `{
			"spec": {
				"containers": [{
					"name": "curl",
					"image": "curlimages/curl:7.87.0",
					"command": ["sh", "-c", "sleep 3600"],
					"securityContext": {
						"allowPrivilegeEscalation": false,
						"capabilities": {
							"drop": ["ALL"]
						},
						"runAsNonRoot": true,
						"runAsUser": 1000,
						"seccompProfile": {
							"type": "RuntimeDefault"
						}
					}
				}],
				"serviceAccountName": "catalogd-controller-manager"
			}
		}`)
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Error creating curl pod: %s", string(output))

	defer func() {
		t.Log("Cleaning up curl pod")
		_ = exec.Command(client, "delete", "pod", curlPod, "-n", namespace, "--ignore-not-found=true").Run()
	}()

	t.Log("Waiting for the curl pod to become ready")
	waitCmd := exec.Command(client, "wait", "--for=condition=Ready", "pod", curlPod, "-n", namespace, "--timeout=60s")
	waitOutput, waitErr := waitCmd.CombinedOutput()
	require.NoError(t, waitErr, "Error waiting for curl pod to be ready: %s", string(waitOutput))

	t.Log("Validating the metrics endpoint")
	metricsURL := "https://catalogd-service." + namespace + ".svc.cluster.local:7443/metrics"
	curlCmd := exec.Command(client, "exec", curlPod, "-n", namespace, "--",
		"curl", "-v", "-k", "-H", "Authorization: Bearer "+token, metricsURL)
	output, err = curlCmd.CombinedOutput()
	require.NoError(t, err, "Error calling metrics endpoint: %s", string(output))
	require.Contains(t, string(output), "200 OK", "Metrics endpoint did not return 200 OK")
}

func stdoutAndCombined(cmd *exec.Cmd) ([]byte, []byte, error) {
	var outOnly bytes.Buffer
	var outAndErr bytes.Buffer
	allWriter := io.MultiWriter(&outOnly, &outAndErr)
	cmd.Stderr = &outAndErr
	cmd.Stdout = allWriter
	err := cmd.Run()
	return outOnly.Bytes(), outAndErr.Bytes(), err
}
