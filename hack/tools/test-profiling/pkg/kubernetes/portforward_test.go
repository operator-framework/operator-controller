package kubernetes

import (
	"testing"
)

func TestNewPortForwarder(t *testing.T) {
	pf := NewPortForwarder("test-namespace", "deployment/test-deployment", 8080, 9090)

	if pf == nil {
		t.Fatal("Expected non-nil PortForwarder")
	}

	if pf.namespace != "test-namespace" {
		t.Errorf("Expected namespace test-namespace, got %s", pf.namespace)
	}

	if pf.deployment != "deployment/test-deployment" {
		t.Errorf("Expected deployment deployment/test-deployment, got %s", pf.deployment)
	}

	if pf.remotePort != 8080 {
		t.Errorf("Expected remote port 8080, got %d", pf.remotePort)
	}

	if pf.localPort != 9090 {
		t.Errorf("Expected local port 9090, got %d", pf.localPort)
	}
}

func TestPortForwarderStop_BeforeStart(t *testing.T) {
	pf := NewPortForwarder("test-namespace", "deployment/test-deployment", 8080, 9090)

	// Should not panic when stopping before starting
	pf.Stop()
}

// Note: Testing Start() requires a real Kubernetes cluster with the deployment available.
// These would be integration tests. Example structure:
//
// func TestPortForwarder_Integration(t *testing.T) {
//     if testing.Short() {
//         t.Skip("Skipping integration test")
//     }
//     // Setup test cluster and deployment
//     // Test Start(), waitReady(), and Stop()
// }

// Note: Testing WaitForNamespace() and WaitForDeployment() requires a real Kubernetes cluster.
// These would be integration tests that use a test cluster with kubectl configured.
