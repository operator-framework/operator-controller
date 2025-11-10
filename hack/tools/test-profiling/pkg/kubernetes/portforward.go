package kubernetes

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwarder manages port-forwarding to a deployment with automatic reconnection
type PortForwarder struct {
	namespace    string
	deployment   string
	remotePort   int
	localPort    int
	actualPort   int // The actual assigned local port (may differ from localPort if 0 was requested)
	clientset    *kubernetes.Clientset
	config       *rest.Config
	stopChan     chan struct{}
	readyChan    chan struct{}
	forwarder    *portforward.PortForwarder
	isConnected  bool
	reconnecting bool
	mu           sync.RWMutex
	ctx          context.Context
}

// NewPortForwarder creates a new port forwarder
func NewPortForwarder(namespace, deployment string, remotePort, localPort int) *PortForwarder {
	return &PortForwarder{
		namespace:  namespace,
		deployment: deployment,
		remotePort: remotePort,
		localPort:  localPort,
	}
}

// Start starts port-forwarding in the background with automatic reconnection
func (pf *PortForwarder) Start(ctx context.Context) error {
	pf.ctx = ctx

	// Get kubeconfig
	config, err := getKubeConfig()
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}
	pf.config = config

	// Create clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	pf.clientset = clientset

	// Setup channels
	pf.stopChan = make(chan struct{}, 1)
	pf.readyChan = make(chan struct{})

	// Establish initial connection
	if err := pf.connect(ctx); err != nil {
		return err
	}

	// Start connection monitor
	go pf.monitorConnection(ctx)

	return nil
}

// connect establishes a port-forward connection
func (pf *PortForwarder) connect(ctx context.Context) error {
	// Find pod for deployment
	podName, err := pf.findPodForDeployment(ctx)
	if err != nil {
		return fmt.Errorf("failed to find pod: %w", err)
	}

	errChan := make(chan error, 1)
	go func() {
		if err := pf.portForward(ctx, podName); err != nil {
			pf.mu.Lock()
			pf.isConnected = false
			pf.mu.Unlock()
			errChan <- err
		}
	}()

	// Wait for port-forward to be ready
	select {
	case <-pf.readyChan:
		// Ready - get the actual assigned port
		if pf.forwarder != nil {
			ports, err := pf.forwarder.GetPorts()
			if err == nil && len(ports) > 0 {
				pf.actualPort = int(ports[0].Local)
			} else {
				pf.actualPort = pf.localPort
			}
		} else {
			pf.actualPort = pf.localPort
		}

		// Mark as connected
		pf.mu.Lock()
		pf.isConnected = true
		pf.mu.Unlock()

	case err := <-errChan:
		pf.Stop()
		return err
	case <-time.After(30 * time.Second):
		pf.Stop()
		return fmt.Errorf("timeout waiting for port-forward to be ready")
	case <-ctx.Done():
		pf.Stop()
		return ctx.Err()
	}

	// Additional check that the pprof endpoint is accessible
	if err := pf.waitReady(30 * time.Second); err != nil {
		pf.Stop()
		return err
	}

	return nil
}

// monitorConnection monitors the port-forward connection and reconnects if needed
func (pf *PortForwarder) monitorConnection(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pf.stopChan:
			return
		case <-ticker.C:
			// Check if endpoint is accessible
			pf.mu.RLock()
			actualPort := pf.actualPort
			connected := pf.isConnected
			reconnecting := pf.reconnecting
			pf.mu.RUnlock()

			if connected && actualPort > 0 {
				// Test the connection
				endpoint := fmt.Sprintf("http://localhost:%d/debug/pprof/", actualPort)
				resp, err := http.Get(endpoint)
				if err != nil || (resp != nil && resp.StatusCode != http.StatusOK) {
					// Connection lost
					pf.mu.Lock()
					pf.isConnected = false
					pf.mu.Unlock()
					if resp != nil {
						resp.Body.Close()
					}
				} else if resp != nil {
					resp.Body.Close()
				}
			}

			pf.mu.RLock()
			connected = pf.isConnected
			reconnecting = pf.reconnecting
			pf.mu.RUnlock()

			if !connected && !reconnecting {
				pf.mu.Lock()
				pf.reconnecting = true
				pf.mu.Unlock()

				fmt.Fprintf(os.Stderr, "🔄 Port-forward disconnected, attempting reconnection...\n")

				// Try to reconnect
				if err := pf.reconnect(ctx); err != nil {
					fmt.Fprintf(os.Stderr, "⚠️  Reconnection failed: %v (will retry)\n", err)
				} else {
					fmt.Fprintf(os.Stderr, "✅ Port-forward reconnected successfully\n")
				}

				pf.mu.Lock()
				pf.reconnecting = false
				pf.mu.Unlock()
			}
		}
	}
}

// reconnect attempts to re-establish the port-forward connection
func (pf *PortForwarder) reconnect(ctx context.Context) error {
	// Close existing forwarder if any
	if pf.stopChan != nil {
		select {
		case <-pf.stopChan:
			// Already closed
		default:
			close(pf.stopChan)
		}
	}

	// Reset channels
	pf.stopChan = make(chan struct{}, 1)
	pf.readyChan = make(chan struct{})

	// Re-establish connection
	return pf.connect(ctx)
}

// Stop stops the port-forwarding
func (pf *PortForwarder) Stop() {
	if pf.stopChan != nil {
		close(pf.stopChan)
	}
}

// GetLocalPort returns the actual assigned local port
func (pf *PortForwarder) GetLocalPort() int {
	pf.mu.RLock()
	defer pf.mu.RUnlock()
	return pf.actualPort
}

// IsConnected returns whether the port-forward is currently connected
func (pf *PortForwarder) IsConnected() bool {
	pf.mu.RLock()
	defer pf.mu.RUnlock()
	return pf.isConnected
}

// findPodForDeployment finds a running pod for the given deployment
func (pf *PortForwarder) findPodForDeployment(ctx context.Context) (string, error) {
	// Get the deployment to find its label selector
	deploymentsClient := pf.clientset.AppsV1().Deployments(pf.namespace)
	deployment, err := deploymentsClient.Get(ctx, pf.deployment, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get deployment: %w", err)
	}

	// List pods matching the deployment's selector
	labelSelector := metav1.FormatLabelSelector(deployment.Spec.Selector)
	pods, err := pf.clientset.CoreV1().Pods(pf.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no running pods found for deployment %s", pf.deployment)
	}

	// Return the first running pod
	return pods.Items[0].Name, nil
}

// portForward establishes the port forward connection
func (pf *PortForwarder) portForward(ctx context.Context, podName string) error {
	// Build URL for port forward
	req := pf.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(pf.namespace).
		Name(podName).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(pf.config)
	if err != nil {
		return fmt.Errorf("failed to create round tripper: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	ports := []string{fmt.Sprintf("%d:%d", pf.localPort, pf.remotePort)}

	// Create port forwarder
	fw, err := portforward.New(dialer, ports, pf.stopChan, pf.readyChan, io.Discard, os.Stderr)
	if err != nil {
		return fmt.Errorf("failed to create port forwarder: %w", err)
	}
	pf.forwarder = fw

	// Start forwarding
	return fw.ForwardPorts()
}

// waitReady waits for the port-forward to be ready by checking the pprof endpoint
func (pf *PortForwarder) waitReady(timeout time.Duration) error {
	endpoint := fmt.Sprintf("http://localhost:%d/debug/pprof/", pf.actualPort)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := http.Get(endpoint)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("port-forward not ready after %v", timeout)
}

// getKubeConfig returns the kubernetes config
func getKubeConfig() (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Fall back to kubeconfig file
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	return config, nil
}

// WaitForNamespace waits for a namespace to exist
func WaitForNamespace(ctx context.Context, namespace string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	lastProgress := time.Now()
	var clientset *kubernetes.Clientset

	for time.Now().Before(deadline) {
		// Try to get kubeconfig (might not exist yet if cluster is starting)
		if clientset == nil {
			config, err := getKubeConfig()
			if err != nil {
				// Kubeconfig doesn't exist yet, wait and retry
				if time.Since(lastProgress) >= 15*time.Second {
					remaining := time.Until(deadline).Round(time.Second)
					fmt.Printf("   Waiting for kubeconfig... (timeout in %v)\n", remaining)
					lastProgress = time.Now()
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(2 * time.Second):
					continue
				}
			}

			var err2 error
			clientset, err2 = kubernetes.NewForConfig(config)
			if err2 != nil {
				// Config exists but client creation failed, wait and retry
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(2 * time.Second):
					continue
				}
			}
		}

		// Try to get the namespace
		_, err := clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
		if err == nil {
			return nil
		}

		// Print progress every 15 seconds
		if time.Since(lastProgress) >= 15*time.Second {
			remaining := time.Until(deadline).Round(time.Second)
			fmt.Printf("   Still waiting for namespace %s... (timeout in %v)\n", namespace, remaining)
			lastProgress = time.Now()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	return fmt.Errorf("namespace %s not found after %v", namespace, timeout)
}

// WaitForDeployment waits for a deployment to be ready
func WaitForDeployment(ctx context.Context, namespace, deployment string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	lastProgress := time.Now()
	var clientset *kubernetes.Clientset

	for time.Now().Before(deadline) {
		// Try to get kubeconfig (might not exist yet if cluster is starting)
		if clientset == nil {
			config, err := getKubeConfig()
			if err != nil {
				// Kubeconfig doesn't exist yet, wait and retry
				if time.Since(lastProgress) >= 15*time.Second {
					remaining := time.Until(deadline).Round(time.Second)
					fmt.Printf("   Waiting for kubeconfig... (timeout in %v)\n", remaining)
					lastProgress = time.Now()
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(2 * time.Second):
					continue
				}
			}

			var err2 error
			clientset, err2 = kubernetes.NewForConfig(config)
			if err2 != nil {
				// Config exists but client creation failed, wait and retry
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(2 * time.Second):
					continue
				}
			}
		}

		dep, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deployment, metav1.GetOptions{})
		if err != nil {
			// Print progress every 15 seconds even if deployment doesn't exist yet
			if time.Since(lastProgress) >= 15*time.Second {
				remaining := time.Until(deadline).Round(time.Second)
				fmt.Printf("   Still waiting for deployment %s... (timeout in %v)\n", deployment, remaining)
				lastProgress = time.Now()
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
				continue
			}
		}

		// Check if deployment is available
		for _, cond := range dep.Status.Conditions {
			if cond.Type == "Available" && cond.Status == corev1.ConditionTrue {
				return nil
			}
		}

		// Print progress every 15 seconds
		if time.Since(lastProgress) >= 15*time.Second {
			remaining := time.Until(deadline).Round(time.Second)
			ready := dep.Status.ReadyReplicas
			desired := dep.Status.Replicas
			fmt.Printf("   Deployment %s: %d/%d replicas ready (timeout in %v)\n", deployment, ready, desired, remaining)
			lastProgress = time.Now()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	return fmt.Errorf("deployment %s not ready after %v", deployment, timeout)
}
