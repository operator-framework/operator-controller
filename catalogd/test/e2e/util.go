package e2e

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	catalogdv1 "github.com/operator-framework/catalogd/api/v1"
)

func ReadTestCatalogServerContents(ctx context.Context, catalog *catalogdv1.ClusterCatalog, c client.Client, kubeClient kubernetes.Interface) ([]byte, error) {
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
