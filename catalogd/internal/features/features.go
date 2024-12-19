package features

import (
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"
)

var catalogdFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{}

var CatalogdFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

func init() {
	utilruntime.Must(CatalogdFeatureGate.Add(catalogdFeatureGates))
}
