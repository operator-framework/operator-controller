package testutils

import (
	"os/exec"
	"testing"
)

// FindK8sClient returns the first available Kubernetes CLI client from the system,
// It checks for the existence of each client by running `version --client`.
// If no suitable client is found, the function terminates the test with a failure.
func FindK8sClient(t *testing.T) string {
	t.Logf("Finding kubectl client")
	clients := []string{"kubectl", "oc"}
	for _, c := range clients {
		// Would prefer to use `command -v`, but even that may not be installed!
		if err := exec.Command(c, "version", "--client").Run(); err == nil {
			t.Logf("Using %q as k8s client", c)
			return c
		}
	}
	t.Fatal("k8s client not found")
	return ""
}
