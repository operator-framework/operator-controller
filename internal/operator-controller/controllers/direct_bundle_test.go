package controllers_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
	"github.com/operator-framework/operator-controller/internal/operator-controller/controllers"
	"github.com/operator-framework/operator-controller/internal/operator-controller/features"
)

func TestDirectBundleRequiresBoxcutter(t *testing.T) {
	previous := features.OperatorControllerFeatureGate.Enabled(features.BoxcutterRuntime)
	t.Cleanup(func() {
		_ = features.OperatorControllerFeatureGate.Set(string(features.BoxcutterRuntime) + "=" + boolString(previous))
	})

	ext := &ocv1.ClusterExtension{Spec: ocv1.ClusterExtensionSpec{Source: ocv1.SourceConfig{SourceType: ocv1.SourceTypeOCIImage}}}
	validator := controllers.DirectBundleRequiresBoxcutter()

	require.NoError(t, features.OperatorControllerFeatureGate.Set(string(features.BoxcutterRuntime)+"=false"))
	require.Error(t, validator(context.Background(), ext))

	require.NoError(t, features.OperatorControllerFeatureGate.Set(string(features.BoxcutterRuntime)+"=true"))
	require.NoError(t, validator(context.Background(), ext))
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
