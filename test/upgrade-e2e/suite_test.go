package upgradee2e

import (
	"os"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/scheme"
)

var (
	cfg        *rest.Config
	c          client.Client
	kclientset *kubernetes.Clientset
)

// testClusterCatalogName and testClusterExtensionName are set via environment variables
// by the Makefile targets (e.g., test-st2ex-e2e) or default to these values.
var (
	testClusterCatalogName   = getEnvOrDefault("TEST_CLUSTER_CATALOG_NAME", "test-catalog")
	testClusterExtensionName = getEnvOrDefault("TEST_CLUSTER_EXTENSION_NAME", "test-package")
)

func init() {
	var err error

	cfg = ctrl.GetConfigOrDie()

	c, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		panic(err)
	}

	kclientset, err = kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(err)
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
