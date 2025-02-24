package utils

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes"

	catalogdv1 "github.com/operator-framework/operator-controller/api/catalogd/v1"
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

func ReadTestCatalogServerContents(ctx context.Context, catalog *catalogdv1.ClusterCatalog, kubeClient kubernetes.Interface) ([]byte, error) {
	if catalog == nil {
		return nil, fmt.Errorf("cannot read nil catalog")
	}
	if catalog.Status.URLs == nil {
		return nil, fmt.Errorf("catalog %q has no catalog urls", catalog.Name)
	}
	url, err := url.Parse(catalog.Status.URLs.Base)
	if err != nil {
		return nil, fmt.Errorf("error parsing clustercatalog url %q: %v", catalog.Status.URLs.Base, err)
	}
	// url is expected to be in the format of
	// http://{service_name}.{namespace}.svc/catalogs/{catalog_name}/
	// so to get the namespace and name of the service we grab only
	// the hostname and split it on the '.' character
	ns := strings.Split(url.Hostname(), ".")[1]
	name := strings.Split(url.Hostname(), ".")[0]
	port := url.Port()
	// the ProxyGet() call below needs an explicit port value, so if
	// value from url.Port() is empty, we assume port 443.
	if port == "" {
		if url.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	resp := kubeClient.CoreV1().Services(ns).ProxyGet(url.Scheme, name, port, url.JoinPath("api", "v1", "all").Path, map[string]string{})
	rc, err := resp.Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	return io.ReadAll(rc)
}
