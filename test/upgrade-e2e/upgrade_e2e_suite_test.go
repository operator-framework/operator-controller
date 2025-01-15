package upgradee2e

import (
	"fmt"
	"os"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/scheme"
)

const (
	testClusterCatalogNameEnv   = "TEST_CLUSTER_CATALOG_NAME"
	testClusterExtensionNameEnv = "TEST_CLUSTER_EXTENSION_NAME"
)

var (
	c          client.Client
	kclientset kubernetes.Interface

	cfg                      *rest.Config
	testClusterCatalogName   string
	testClusterExtensionName string
)

func TestMain(m *testing.M) {
	var ok bool
	cfg = ctrl.GetConfigOrDie()
	testClusterCatalogName, ok = os.LookupEnv(testClusterCatalogNameEnv)
	if !ok {
		fmt.Printf("%q is not set", testClusterCatalogNameEnv)
		os.Exit(1)
	}
	testClusterExtensionName, ok = os.LookupEnv(testClusterExtensionNameEnv)
	if !ok {
		fmt.Printf("%q is not set", testClusterExtensionNameEnv)
		os.Exit(1)
	}

	cfg := ctrl.GetConfigOrDie()

	var err error
	c, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		fmt.Printf("failed to create client: %s\n", err)
		os.Exit(1)
	}

	kclientset, err = kubernetes.NewForConfig(cfg)
	if err != nil {
		fmt.Printf("failed to create kubernetes clientset: %s\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}
