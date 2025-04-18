package features

import (
	"sort"

	"github.com/go-logr/logr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"
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
	// Sort the keys for consistent logging order
	featureKeys := make([]featuregate.Feature, 0, len(catalogdFeatureGates))
	for k := range catalogdFeatureGates {
		featureKeys = append(featureKeys, k)
	}
	sort.Slice(featureKeys, func(i, j int) bool {
		return string(featureKeys[i]) < string(featureKeys[j])
	})

	featurePairs := make([]interface{}, 0, len(featureKeys))
	for _, feature := range featureKeys {
		featurePairs = append(featurePairs, feature, fg.Enabled(feature))
	}
	log.Info("catalogd feature gate status", featurePairs...)
}
