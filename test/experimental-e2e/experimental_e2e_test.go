package experimental_e2e

import (
	"os"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operator-framework/operator-controller/internal/operator-controller/scheme"
	"github.com/operator-framework/operator-controller/test/utils"
)

const (
	artifactName = "operator-controller-experimental-e2e"
)

var (
	cfg *rest.Config
	c   client.Client
)

func TestMain(m *testing.M) {
	cfg = ctrl.GetConfigOrDie()

	var err error
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme.Scheme))
	c, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	utilruntime.Must(err)

	os.Exit(m.Run())
}

func TestNoop(t *testing.T) {
	t.Log("Running experimental-e2e tests")
	defer utils.CollectTestArtifacts(t, artifactName, c, cfg)
}
