package featuregates_test

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"k8s.io/component-base/featuregate"

	"github.com/operator-framework/operator-controller/internal/shared/util/featuregates"
	mocklogrsink "github.com/operator-framework/operator-controller/internal/testutil/mock/logrsink"
)

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

	// prepare a mock sink and logger, capturing Info calls
	ctrl := gomock.NewController(t)
	sink := mocklogrsink.NewMockLogSink(ctrl)

	var capturedMsg string
	var capturedKV []interface{}

	sink.EXPECT().Init(gomock.Any()).AnyTimes()
	sink.EXPECT().Enabled(gomock.Any()).Return(true).AnyTimes()
	sink.EXPECT().Info(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(level int, msg string, kv ...interface{}) {
			capturedMsg = msg
			capturedKV = append([]interface{}{}, kv...)
		}).AnyTimes()
	sink.EXPECT().Error(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	sink.EXPECT().WithValues(gomock.Any()).Return(sink).AnyTimes()
	sink.EXPECT().WithName(gomock.Any()).Return(sink).AnyTimes()

	logger := logr.New(sink)

	// log the feature states
	featuregates.LogFeatureGateStates(logger, "feature states", gate, defs)

	// verify the message
	require.Equal(t, "feature states", capturedMsg)

	// Expect keys sorted: AFeature, BFeature, CFeature
	want := []interface{}{
		featuregate.Feature("AFeature"), false,
		featuregate.Feature("BFeature"), true,
		featuregate.Feature("CFeature"), true,
	}
	require.Equal(t, want, capturedKV)
}
