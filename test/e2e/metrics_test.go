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
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// FetchOperatorControllerMetricsExportedEndpoint verifies that the metrics endpoint for the operator controller
func FetchOperatorControllerMetricsExportedEndpoint(t *testing.T) {
	kubeClient, restConfig := findK8sClient(t)
	mtc := NewMetricsTestConfig(
		t,
		kubeClient,
		restConfig,
		"control-plane=operator-controller-controller-manager",
		"operator-controller-metrics-reader",
		"operator-controller-metrics-binding",
		"operator-controller-controller-manager",
		"oper-curl-metrics",
		"https://operator-controller-service.NAMESPACE.svc.cluster.local:8443/metrics",
		"oper-con",
	)

	mtc.run()
}

// FetchCatalogdMetricsExportedEndpoint verifies that the metrics endpoint for catalogd
func FetchCatalogdMetricsExportedEndpoint(t *testing.T) {
	kubeClient, restConfig := findK8sClient(t)
	mtc := NewMetricsTestConfig(
		t,
		kubeClient,
		restConfig,
		"control-plane=catalogd-controller-manager",
		"catalogd-metrics-reader",
		"catalogd-metrics-binding",
		"catalogd-controller-manager",
		"catalogd-curl-metrics",
		"https://catalogd-service.NAMESPACE.svc.cluster.local:7443/metrics",
		"catalogd",
	)

	mtc.run()
}

// fetchMetrics retrieves Prometheus metrics from the endpoint
func (c *MetricsTestConfig) fetchMetrics(ctx context.Context, token string) string {
	c.t.Log("Fetching Prometheus metrics after test execution")

	// Command to execute inside the pod
	cmd := []string{
		"curl", "-s", "-k",
		"-H", "Authorization: Bearer " + token,
		c.metricsURL,
	}

	// Execute command in pod
	req := c.kubeClient.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Namespace(c.namespace).
		Name(c.curlPodName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "curl",
			Command:   cmd,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(c.restConfig, "POST", req.URL())
	require.NoError(c.t, err, "Error creating SPDY executor")

	var stdout, stderr bytes.Buffer
	streamOpts := remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}

	err = executor.StreamWithContext(ctx, streamOpts)
	require.NoError(c.t, err, "Error streaming exec request: %v", stderr.String())

	return stdout.String()
}

// saveMetricsToFile writes the fetched metrics to a text file
func (c *MetricsTestConfig) saveMetricsToFile(metrics string) {
	dir := "results"
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		c.t.Fatalf("Failed to create directory %s: %v", dir, err)
	}

	filePath := fmt.Sprintf("%s/metrics_%s_%s.txt", dir, c.name, c.t.Name())
	err := os.WriteFile(filePath, []byte(metrics), 0644)
	require.NoError(c.t, err, "Failed to save metrics to file")

	c.t.Logf("Metrics saved to: %s", filePath)
}

func findK8sClient(t *testing.T) (kubernetes.Interface, *rest.Config) {
	cfg, err := config.GetConfig()
	require.NoError(t, err, "Failed to get Kubernetes config")

	clientset, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err, "Failed to create client from config")

	t.Log("Successfully created Kubernetes client via controller-runtime config")
	return clientset, cfg
}

// MetricsTestConfig holds the necessary configurations for testing metrics endpoints.
type MetricsTestConfig struct {
	t              *testing.T
	kubeClient     kubernetes.Interface
	restConfig     *rest.Config
	namespace      string
	clusterRole    string
	clusterBinding string
	serviceAccount string
	curlPodName    string
	metricsURL     string
	name           string
}

// NewMetricsTestConfig initializes a new MetricsTestConfig.
func NewMetricsTestConfig(
	t *testing.T,
	kubeClient kubernetes.Interface,
	restConfig *rest.Config,
	selector string,
	clusterRole string,
	clusterBinding string,
	serviceAccount string,
	curlPodName string,
	metricsURL string,
	name string,
) *MetricsTestConfig {
	// Discover which namespace the relevant Pod is running in
	namespace := getComponentNamespace(t, kubeClient, selector)

	// Replace the placeholder in the metrics URL
	metricsURL = strings.ReplaceAll(metricsURL, "NAMESPACE", namespace)

	return &MetricsTestConfig{
		t:              t,
		kubeClient:     kubeClient,
		restConfig:     restConfig,
		namespace:      namespace,
		clusterRole:    clusterRole,
		clusterBinding: clusterBinding,
		serviceAccount: serviceAccount,
		curlPodName:    curlPodName,
		metricsURL:     metricsURL,
		name:           name,
	}
}

// run executes the entire test flow
func (c *MetricsTestConfig) run() {
	ctx := context.Background()
	// To speed up
	// defer c.cleanup(ctx)
	c.createMetricsClusterRoleBinding(ctx)
	token := c.getServiceAccountToken(ctx)
	c.createCurlMetricsPod(ctx)
	c.waitForPodReady(ctx)
	// Exec `curl` in the Pod to validate the metrics
	c.validateMetricsEndpoint(ctx, token)

	// Fetch and save Prometheus metrics after test execution
	metrics := c.fetchMetrics(ctx, token)
	c.saveMetricsToFile(metrics)
}

// createMetricsClusterRoleBinding to bind the cluster role so metrics are accessible
func (c *MetricsTestConfig) createMetricsClusterRoleBinding(ctx context.Context) {
	c.t.Logf("Ensuring ClusterRoleBinding %q exists in namespace %q", c.clusterBinding, c.namespace)

	_, err := c.kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, c.clusterBinding, metav1.GetOptions{})
	if err == nil {
		c.t.Logf("ClusterRoleBinding %q already exists, skipping creation", c.clusterBinding)
		return
	}

	if !apierrors.IsNotFound(err) {
		require.NoError(c.t, err, "Error checking for existing ClusterRoleBinding")
		return
	}

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: c.clusterBinding,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      c.serviceAccount,
				Namespace: c.namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     c.clusterRole,
		},
	}

	_, err = c.kubeClient.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{})
	require.NoError(c.t, err, "Error creating ClusterRoleBinding")
	c.t.Logf("Successfully created ClusterRoleBinding %q", c.clusterBinding)
}

// getServiceAccountToken creates a TokenRequest for the service account
func (c *MetricsTestConfig) getServiceAccountToken(ctx context.Context) string {
	c.t.Logf("Generating ServiceAccount token in namespace %q", c.namespace)

	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			Audiences:         []string{"https://kubernetes.default.svc.cluster.local"},
			ExpirationSeconds: nil,
		},
	}

	tr, err := c.kubeClient.CoreV1().
		ServiceAccounts(c.namespace).
		CreateToken(ctx, c.serviceAccount, tokenRequest, metav1.CreateOptions{})
	require.NoError(c.t, err, "Error requesting token for SA %q", c.serviceAccount)

	token := tr.Status.Token
	require.NotEmpty(c.t, token, "ServiceAccount token was empty")
	return token
}

// createCurlMetricsPod spawns a pod running `curlimages/curl` to check metrics
func (c *MetricsTestConfig) createCurlMetricsPod(ctx context.Context) {
	c.t.Logf("Ensuring curl pod (%s/%s) exists to validate the metrics endpoint", c.namespace, c.curlPodName)

	_, err := c.kubeClient.CoreV1().Pods(c.namespace).Get(ctx, c.curlPodName, metav1.GetOptions{})
	if err == nil {
		c.t.Logf("Curl pod %q already exists, skipping creation", c.curlPodName)
		return
	}

	if !apierrors.IsNotFound(err) {
		require.NoError(c.t, err, "Error checking for existing curl pod")
		return
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.curlPodName,
			Namespace: c.namespace,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName:            c.serviceAccount,
			TerminationGracePeriodSeconds: ptr.To(int64(0)),
			Containers: []corev1.Container{
				{
					Name:    "curl",
					Image:   "curlimages/curl",
					Command: []string{"sh", "-c", "sleep 3600"},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: ptr.To(false),
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
						RunAsNonRoot: ptr.To(true),
						RunAsUser:    ptr.To(int64(1000)),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	_, err = c.kubeClient.CoreV1().Pods(c.namespace).Create(ctx, pod, metav1.CreateOptions{})
	require.NoError(c.t, err, "Error creating curl pod")
}

// waitForPodReady polls until the Pod is in Ready condition
func (c *MetricsTestConfig) waitForPodReady(ctx context.Context) {
	c.t.Log("Waiting for the curl pod to be ready")
	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 60*time.Second, false, func(ctx context.Context) (bool, error) {
		pod, err := c.kubeClient.CoreV1().Pods(c.namespace).Get(ctx, c.curlPodName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
	if errors.Is(err, context.DeadlineExceeded) {
		c.t.Fatal("Timed out waiting for the curl pod to become Ready")
	}
	require.NoError(c.t, err, "Error waiting for curl pod to become Ready")
}

// validateMetricsEndpoint performs `kubectl exec ... curl <metricsURL>` logic
func (c *MetricsTestConfig) validateMetricsEndpoint(ctx context.Context, token string) {
	c.t.Log("Validating the metrics endpoint via pod exec")

	// The command to run inside the container
	cmd := []string{
		"curl", "-v", "-k",
		"-H", "Authorization: Bearer " + token,
		c.metricsURL,
	}

	// Construct the request to exec into the pod
	req := c.kubeClient.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Namespace(c.namespace).
		Name(c.curlPodName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "curl",
			Command:   cmd,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	// Create an SPDY executor
	executor, err := remotecommand.NewSPDYExecutor(c.restConfig, "POST", req.URL())
	require.NoError(c.t, err, "Error creating SPDY executor to exec in pod")

	var stdout, stderr bytes.Buffer
	streamOpts := remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	}

	err = executor.StreamWithContext(ctx, streamOpts)
	require.NoError(c.t, err, "Error streaming exec request: %v", stderr.String())

	// Combine stdout + stderr
	combined := stdout.String() + stderr.String()
	require.Contains(c.t, combined, "200 OK", "Metrics endpoint did not return 200 OK")
}

// cleanup deletes the test resources
func (c *MetricsTestConfig) cleanup(ctx context.Context) {
	c.t.Log("Cleaning up resources")
	policy := metav1.DeletePropagationForeground

	// Delete the ClusterRoleBinding
	_ = c.kubeClient.RbacV1().ClusterRoleBindings().Delete(ctx, c.clusterBinding, metav1.DeleteOptions{
		PropagationPolicy: &policy,
	})
	waitForClusterRoleBindingDeletion(ctx, c.t, c.kubeClient, c.clusterBinding)

	// "Force" delete the Pod by setting grace period to 0
	gracePeriod := int64(0)
	_ = c.kubeClient.CoreV1().Pods(c.namespace).Delete(ctx, c.curlPodName, metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &policy,
	})
	waitForPodDeletion(ctx, c.t, c.kubeClient, c.namespace, c.curlPodName)
}

// waitForClusterRoleBindingDeletion polls until the named ClusterRoleBinding no longer exists
func waitForClusterRoleBindingDeletion(ctx context.Context, t *testing.T, kubeClient kubernetes.Interface, name string) {
	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 60*time.Second, false, func(ctx context.Context) (bool, error) {
		_, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Timed out waiting for ClusterRoleBinding %q to be deleted", name)
		}
		t.Logf("Error waiting for ClusterRoleBinding %q deletion: %v", name, err)
	} else {
		t.Logf("ClusterRoleBinding %q deleted", name)
	}
}

// waitForPodDeletion polls until the named Pod no longer exists
func waitForPodDeletion(ctx context.Context, t *testing.T, kubeClient kubernetes.Interface, namespace, name string) {
	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 90*time.Second, false, func(ctx context.Context) (bool, error) {
		pod, getErr := kubeClient.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			if apierrors.IsNotFound(getErr) {
				return true, nil
			}
			return false, getErr
		}
		// Some extra log info if the Pod is still around
		t.Logf("Pod %q still present, phase=%q, deleting... (Timestamp=%v)",
			name, pod.Status.Phase, pod.DeletionTimestamp)
		return false, nil
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Timed out waiting for Pod %q to be deleted", name)
		}
		t.Logf("Error waiting for Pod %q deletion: %v", name, err)
	} else {
		t.Logf("Pod %q deleted", name)
	}
}

// getComponentNamespace identifies which Namespace is running a Pod that matches `selector`
func getComponentNamespace(t *testing.T, kubeClient kubernetes.Interface, selector string) string {
	t.Logf("Listing pods for selector %q to discover namespace", selector)
	ctx := context.Background()

	pods, err := kubeClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	require.NoError(t, err, "Error listing pods for selector %q", selector)
	require.NotEmpty(t, pods.Items, "No pods found for selector %q", selector)

	namespace := pods.Items[0].Namespace
	if namespace == "" {
		t.Fatalf("No namespace found for selector %q", selector)
	}
	return namespace
}
