package action

import (
	"log"
	"os"
	"testing"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"

	test "github.com/operator-framework/operator-controller/internal/testutil"
)

var cfg *rest.Config

func TestMain(m *testing.M) {
	testEnv := test.NewEnv()

	var err error
	cfg, err = testEnv.Start()
	utilruntime.Must(err)
	if cfg == nil {
		log.Panic("expected cfg to not be nil")
	}

	code := m.Run()
	stopErr := test.StopWithRetry(testEnv, time.Minute, time.Second)
	utilruntime.Must(stopErr)
	os.Exit(code)
}
