package features

import (
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"
)

const (
	// Add new feature gates constants (strings)
	// Ex: SomeFeature featuregate.Feature = "SomeFeature"
	PreflightPermissions featuregate.Feature = "PreflightPermissions"
)

var operatorControllerFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	// Add new feature gate definitions
	// if you're adding a feature gate for fellow developers to use in ongoing development, use PreAlpha
	// Ex: SomeFeature: {...}
	PreflightPermissions: {
		Default:       false,
		PreRelease:    featuregate.PreAlpha, // keep this PreAlpha until done with feature development
		LockToDefault: false,
	},
}

var OperatorControllerFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

func init() {
	utilruntime.Must(OperatorControllerFeatureGate.Add(operatorControllerFeatureGates))
}
