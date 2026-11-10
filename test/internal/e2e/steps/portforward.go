package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func randomAvailablePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// PortForward starts a kubectl port-forward to target (e.g. "service/foo" or "pod/bar")
// in the given namespace, mapping a random local port to remotePort. It returns the
// local address and a cleanup function. The caller is responsible for calling cleanup.
func PortForward(ctx context.Context, ns, target string, remotePort int32) (string, func(), error) {
	localPort, err := randomAvailablePort()
	if err != nil {
		return "", nil, fmt.Errorf("failed to find a free local port: %w", err)
	}

	pfCmd := exec.Command(K8sCLI, "port-forward", "-n", ns, //nolint:gosec
		target, fmt.Sprintf("%d:%d", localPort, remotePort))
	pfCmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", KubeconfigPath))
	if err := pfCmd.Start(); err != nil {
		return "", nil, fmt.Errorf("failed to start port-forward to %s: %w", target, err)
	}

	cleanup := func() {
		if p := pfCmd.Process; p != nil {
			_ = p.Kill()
			_ = pfCmd.Wait()
		}
	}

	if rec := CommandRecorderFromContext(ctx); rec != nil {
		rec.RecordCustom(
			fmt.Sprintf("%s port-forward -n %s %s %d:%d &", K8sCLI, ns, target, localPort, remotePort),
			"", "",
		)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", localPort)
	return addr, cleanup, nil
}

// GetMetricsService returns the full Service object for the named component's metrics service.
// The namespace is available via svc.Namespace.
func GetMetricsService(component string) (*corev1.Service, error) {
	serviceName := fmt.Sprintf("%s-service", component)
	serviceNs, err := K8sClient(context.Background(), "get", "service", "-A", "-o",
		fmt.Sprintf(`jsonpath={.items[?(@.metadata.name=="%s")].metadata.namespace}`, serviceName))
	if err != nil {
		return nil, fmt.Errorf("failed to find namespace for service %s: %w", serviceName, err)
	}
	serviceNs = strings.TrimSpace(serviceNs)
	if serviceNs == "" {
		return nil, fmt.Errorf("service %s not found in any namespace", serviceName)
	}

	raw, err := K8sClient(context.Background(), "get", "service", "-n", serviceNs, serviceName, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("failed to get service %s: %w", serviceName, err)
	}
	var svc corev1.Service
	if err := json.Unmarshal([]byte(raw), &svc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal service %s: %w", serviceName, err)
	}
	return &svc, nil
}

// MetricsPort returns the port number of the port named "metrics" on the given service.
func MetricsPort(svc *corev1.Service) (int32, error) {
	for _, p := range svc.Spec.Ports {
		if p.Name == "metrics" {
			return p.Port, nil
		}
	}
	return 0, fmt.Errorf("no port named 'metrics' found on service %s", svc.Name)
}
