package features

import (
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
