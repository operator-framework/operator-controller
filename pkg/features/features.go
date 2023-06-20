package features

import (
	"k8s.io/component-base/featuregate"
)

const (
// Add new feature gates constants (strings)
// Ex: SomeFeature featuregate.Feature = "SomeFeature"
)

var operatorControllerFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	// Add new feature gate definitions
	// Ex: SomeFeature: {...}
}

var OperatorControllerFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

func init() {
	OperatorControllerFeatureGate.Add(operatorControllerFeatureGates)
}
