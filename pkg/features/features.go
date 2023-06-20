package features

import (
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"
)

const (
// Add new feature gates constants (strings)
// Ex: SomeFeature featuregate.Feature = "SomeFeature"
)

var catalogdFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	// Add new feature gate definitions
	// Ex: SomeFeature: {...}
}

var CatalogdFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

func init() {
	utilruntime.Must(CatalogdFeatureGate.Add(catalogdFeatureGates))
}
