package features

import (
	"sort"

	"github.com/go-logr/logr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"
)

const (
	// Add new feature gates constants (strings)
	// Ex: SomeFeature featuregate.Feature = "SomeFeature"
	PreflightPermissions             featuregate.Feature = "PreflightPermissions"
	SingleOwnNamespaceInstallSupport featuregate.Feature = "SingleOwnNamespaceInstallSupport"
)

var operatorControllerFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	// Add new feature gate definitions
	// Ex: SomeFeature: {...}
	PreflightPermissions: {
		Default:       false,
		PreRelease:    featuregate.Alpha,
		LockToDefault: false,
	},

	// SingleOwnNamespaceInstallSupport enables support for installing
	// registry+v1 cluster extensions with single or own namespaces modes
	// i.e. with a single watch namespace.
	SingleOwnNamespaceInstallSupport: {
		Default:       false,
		PreRelease:    featuregate.Alpha,
		LockToDefault: false,
	},
}

var OperatorControllerFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

func init() {
	utilruntime.Must(OperatorControllerFeatureGate.Add(operatorControllerFeatureGates))
}

// LogFeatureGateStates logs the state of all known feature gates.
func LogFeatureGateStates(log logr.Logger, fg featuregate.FeatureGate) {
	// Sort the keys for consistent logging order
	featureKeys := make([]featuregate.Feature, 0, len(operatorControllerFeatureGates))
	for k := range operatorControllerFeatureGates {
		featureKeys = append(featureKeys, k)
	}
	sort.Slice(featureKeys, func(i, j int) bool {
		return string(featureKeys[i]) < string(featureKeys[j]) // Sort by string representation
	})

	log.Info("Feature Gates Status:")
	for _, feature := range featureKeys {
		log.Info("  ", "feature", string(feature), "enabled", fg.Enabled(feature))
	}
}
