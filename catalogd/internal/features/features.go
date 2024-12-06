package features

import (
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"
)

const (
	APIV1QueryHandler = featuregate.Feature("APIV1QueryHandler")
)

var catalogdFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	APIV1QueryHandler: {Default: false, PreRelease: featuregate.Alpha},
}

var CatalogdFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

func init() {
	utilruntime.Must(CatalogdFeatureGate.Add(catalogdFeatureGates))
}
