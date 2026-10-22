package features

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/component-base/featuregate"
)

func TestDaemonSetSupportEnabled(t *testing.T) {
	for _, tc := range []struct {
		name      string
		daemonSet bool
		boxcutter bool
		want      bool
	}{
		{name: "both gates enabled", daemonSet: true, boxcutter: true, want: true},
		{name: "rejected without BoxcutterRuntime", daemonSet: true, boxcutter: false, want: false},
		{name: "rejected without RegistryV1DaemonSetSupport", daemonSet: false, boxcutter: true, want: false},
		{name: "both gates disabled", daemonSet: false, boxcutter: false, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fg := featuregate.NewFeatureGate()
			require.NoError(t, fg.Add(operatorControllerFeatureGates))
			require.NoError(t, fg.SetFromMap(map[string]bool{
				string(RegistryV1DaemonSetSupport): tc.daemonSet,
				string(BoxcutterRuntime):           tc.boxcutter,
			}))
			require.Equal(t, tc.want, DaemonSetSupportEnabled(fg))
		})
	}
}
