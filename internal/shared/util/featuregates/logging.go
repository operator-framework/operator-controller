package featuregates

import (
	"sort"

	"github.com/go-logr/logr"
	"k8s.io/component-base/featuregate"
)

// LogFeatureGateStates logs a sorted list of features and their enabled state
// message is the log message under which to record the feature states
// fg is the feature gate instance, and featureDefs is the map of feature specs
func LogFeatureGateStates(log logr.Logger, message string, fg featuregate.FeatureGate, featureDefs map[featuregate.Feature]featuregate.FeatureSpec) {
	// Collect and sort feature keys for deterministic ordering
	keys := make([]featuregate.Feature, 0, len(featureDefs))
	for f := range featureDefs {
		keys = append(keys, f)
	}
	sort.Slice(keys, func(i, j int) bool {
		return string(keys[i]) < string(keys[j])
	})

	// Build key/value pairs for logging
	pairs := make([]interface{}, 0, len(keys)*2)
	for _, f := range keys {
		pairs = append(pairs, f, fg.Enabled(f))
	}
	log.Info(message, pairs...)
}
