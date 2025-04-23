package features

import (
	"github.com/go-logr/logr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"

	fgutil "github.com/operator-framework/operator-controller/internal/shared/util/featuregates"
)

const (
	APIV1MetasHandler = featuregate.Feature("APIV1MetasHandler")
)

var catalogdFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	APIV1MetasHandler: {Default: false, PreRelease: featuregate.Alpha},
}

var CatalogdFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

func init() {
	utilruntime.Must(CatalogdFeatureGate.Add(catalogdFeatureGates))
}

// LogFeatureGateStates logs the state of all known feature gates for catalogd
func LogFeatureGateStates(log logr.Logger, fg featuregate.FeatureGate) {
	fgutil.LogFeatureGateStates(log, "catalogd feature gate status", fg, catalogdFeatureGates)
}
