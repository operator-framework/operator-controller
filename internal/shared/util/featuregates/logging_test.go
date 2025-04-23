package featuregates_test

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"k8s.io/component-base/featuregate"

	"github.com/operator-framework/operator-controller/internal/shared/util/featuregates"
)

// fakeSink implements logr.LogSink, capturing Info calls for testing
type fakeSink struct {
	level         int
	msg           string
	keysAndValues []interface{}
}

// Init is part of logr.LogSink
func (f *fakeSink) Init(info logr.RuntimeInfo) {}

// Enabled is part of logr.LogSink
func (f *fakeSink) Enabled(level int) bool { return true }

// Info captures the log level, message, and key/value pairs
func (f *fakeSink) Info(level int, msg string, keysAndValues ...interface{}) {
	f.level = level
	f.msg = msg
	f.keysAndValues = append([]interface{}{}, keysAndValues...)
}

// Error is part of logr.LogSink; not used in this test
func (f *fakeSink) Error(err error, msg string, keysAndValues ...interface{}) {}

// WithValues returns a sink with additional values; for testing, return self
func (f *fakeSink) WithValues(keysAndValues ...interface{}) logr.LogSink { return f }

// WithName returns a sink with a new name; for testing, return self
func (f *fakeSink) WithName(name string) logr.LogSink { return f }

// TestLogFeatureGateStates verifies that LogFeatureGateStates logs features
// sorted alphabetically with their enabled state
func TestLogFeatureGateStates(t *testing.T) {
	// Define a set of feature specs with default states
	defs := map[featuregate.Feature]featuregate.FeatureSpec{
		"AFeature": {Default: false},
		"BFeature": {Default: true},
		"CFeature": {Default: false},
	}

	// create a mutable gate and register our definitions
	gate := featuregate.NewFeatureGate()
	require.NoError(t, gate.Add(defs))

	// override CFeature to true.
	require.NoError(t, gate.SetFromMap(map[string]bool{
		"CFeature": true,
	}))

	// prepare a fake sink and logger
	sink := &fakeSink{}
	logger := logr.New(sink)

	// log the feature states
	featuregates.LogFeatureGateStates(logger, "feature states", gate, defs)

	// verify the message
	require.Equal(t, "feature states", sink.msg)

	// Expect keys sorted: AFeature, BFeature, CFeature
	want := []interface{}{
		featuregate.Feature("AFeature"), false,
		featuregate.Feature("BFeature"), true,
		featuregate.Feature("CFeature"), true,
	}
	require.Equal(t, want, sink.keysAndValues)
}
