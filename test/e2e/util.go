package e2e

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogd "github.com/operator-framework/catalogd/api/core/v1alpha1"
)

func ReadTestCatalogServerContents(ctx context.Context, catalog *catalogd.ClusterCatalog, c client.Client, kubeClient kubernetes.Interface) ([]byte, error) {
	if catalog == nil {
		return nil, fmt.Errorf("cannot read nil catalog")
	}
	url, err := url.Parse(catalog.Status.ContentURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing clustercatalog url %s: %v", catalog.Status.ContentURL, err)
	}
	// url is expected to be in the format of
	// http://{service_name}.{namespace}.svc/{catalog_name}/all.json
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
	resp := kubeClient.CoreV1().Services(ns).ProxyGet(url.Scheme, name, port, url.Path, map[string]string{})
	rc, err := resp.Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	return io.ReadAll(rc)
}
